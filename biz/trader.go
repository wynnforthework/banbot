package biz

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	ta "github.com/banbox/banta"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"strings"
)

type Trader struct {
}

func (t *Trader) Init() *errs.Error {
	return SetupComs()
}

func (t *Trader) OnEnvJobs(bar *banexg.PairTFKline) (*ta.BarEnv, []*strategy.StagyJob, []*strategy.StagyJob, *errs.Error) {
	envKey := strings.Join([]string{bar.Symbol, bar.TimeFrame}, "_")
	env, ok := strategy.Envs[envKey]
	if !ok {
		return nil, nil, nil, errs.NewMsg(core.ErrBadConfig, "env for %s/%s not found", bar.Symbol, bar.TimeFrame)
	}
	if core.LiveMode() && env.TimeStop > bar.Time {
		// 此bar已过期，忽略，启动时爬虫可能会推已处理的过期bar
		return nil, nil, nil, nil
	}
	// 更新BarEnv状态
	env.OnBar(bar.Time, bar.Open, bar.High, bar.Low, bar.Close, bar.Volume)
	// 获取交易任务
	jobs, ok := strategy.Jobs[envKey]
	// 辅助订阅的任务
	infoJobs, ok := strategy.InfoJobs[envKey]
	return env, jobs, infoJobs, nil
}

func (t *Trader) FeedKline(bar *banexg.PairTFKline) *errs.Error {
	if _, ok := core.ForbidPairs[bar.Symbol]; ok {
		return nil
	}
	isLive := core.LiveMode()
	tfMSecs := int64(utils.TFToSecs(bar.TimeFrame) * 1000)
	core.SetBarPrice(bar.Symbol, bar.Close)
	// 超过1分钟且周期的一半，认为bar延迟，不可下单
	delayMs := btime.TimeMS() - bar.Time - tfMSecs
	barExpired := delayMs >= max(60000, tfMSecs/2)
	if barExpired && isLive && !core.IsWarmUp {
		log.Warn(fmt.Sprintf("%s/%s delay %v s, open order is disabled", bar.Symbol, bar.TimeFrame, delayMs/1000))
	}
	// 更新指标环境
	env, jobs, infoJobs, err := t.OnEnvJobs(bar)
	if err != nil {
		log.Error(fmt.Sprintf("%s/%s OnEnvJobs fail", bar.Symbol, bar.TimeFrame), zap.Error(err))
		return err
	} else if env == nil || len(jobs) == 0 {
		return nil
	}
	// 更新非生产模式的订单
	allOrders := utils.ValsOfMap(orm.OpenODs)
	if !core.IsWarmUp {
		// 这里可能修改订单状态
		err = OdMgr.UpdateByBar(allOrders, bar)
		if err != nil {
			log.Error("update orders by bar fail", zap.Error(err))
			return err
		}
	}
	// 要在UpdateByBar后检索当前开放订单，过滤已平仓订单
	var curOrders []*orm.InOutOrder
	for _, od := range allOrders {
		if od.Status < orm.InOutStatusFullExit && od.Symbol == bar.Symbol && od.Timeframe == bar.TimeFrame {
			curOrders = append(curOrders, od)
		}
	}
	var enters []*strategy.EnterReq
	var exits []*strategy.ExitReq
	var edits []*orm.InOutEdit
	for _, job := range jobs {
		job.InitBar(curOrders)
		job.Stagy.OnBar(job)
		if !barExpired {
			enters = append(enters, job.Entrys...)
		}
		if !core.IsWarmUp {
			curEdits, err := job.CheckCustomExits()
			if err != nil {
				return err
			}
			edits = append(edits, curEdits...)
		}
		exits = append(exits, job.Exits...)
	}
	// 更新辅助订阅数据
	for _, job := range infoJobs {
		job.Stagy.OnInfoBar(job, bar.Symbol, bar.TimeFrame)
	}
	// 处理订单
	if !core.IsWarmUp && len(enters)+len(exits)+len(edits) > 0 {
		var sess *orm.Queries
		var conn *pgxpool.Conn
		if isLive {
			// 非实时模式，订单临时保存到内存，无需数据库
			sess, conn, err = orm.Conn(nil)
			if err != nil {
				log.Error("get db sess fail", zap.Error(err))
				return err
			}
			defer conn.Release()
		}
		var ents []*orm.InOutOrder
		ents, _, err = OdMgr.ProcessOrders(sess, env, enters, exits, edits)
		if err != nil {
			log.Error("process orders fail", zap.Error(err))
			return err
		}
		t.onStagyEnterCB(ents, jobs)
	}
	return nil
}

func (t *Trader) onStagyEnterCB(ents []*orm.InOutOrder, jobs []*strategy.StagyJob) {
	if len(ents) == 0 {
		return
	}
	var jobMap = map[string]*strategy.StagyJob{}
	for _, job := range jobs {
		jobMap[job.Stagy.Name] = job
	}
	for _, od := range ents {
		job, ok := jobMap[od.Strategy]
		if !ok || job.Stagy.OnOrderChange == nil {
			continue
		}
		job.Stagy.OnOrderChange(job, od, strategy.OdChgEnter)
	}
}
