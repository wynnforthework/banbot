package biz

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
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

func (t *Trader) OnEnvJobs(bar *orm.InfoKline) (*ta.BarEnv, *errs.Error) {
	envKey := strings.Join([]string{bar.Symbol, bar.TimeFrame}, "_")
	env, ok := strategy.Envs[envKey]
	if !ok {
		return nil, errs.NewMsg(core.ErrBadConfig, "env for %s/%s not found", bar.Symbol, bar.TimeFrame)
	}
	if core.LiveMode && env.TimeStop > bar.Time {
		// 此bar已过期，忽略，启动时爬虫可能会推已处理的过期bar
		return nil, nil
	}
	// 更新BarEnv状态
	env.OnBar(bar.Time, bar.Open, bar.High, bar.Low, bar.Close, bar.Volume, bar.Info)
	return env, nil
}

func (t *Trader) FeedKline(bar *orm.InfoKline) *errs.Error {
	if _, ok := core.ForbidPairs[bar.Symbol]; ok {
		return nil
	}
	tfSecs := utils.TFToSecs(bar.TimeFrame)
	core.SetBarPrice(bar.Symbol, bar.Close)
	// 超过1分钟且周期的一半，认为bar延迟，不可下单
	delaySecs := int((btime.TimeMS()-bar.Time)/1000) - tfSecs
	barExpired := delaySecs >= max(60, tfSecs/2)
	if barExpired && core.LiveMode && !core.IsWarmUp {
		log.Warn(fmt.Sprintf("%s/%s delay %v s, open order is disabled", bar.Symbol, bar.TimeFrame, delaySecs))
	}
	// 更新指标环境
	env, err := t.OnEnvJobs(bar)
	if err != nil {
		log.Error(fmt.Sprintf("%s/%s OnEnvJobs fail", bar.Symbol, bar.TimeFrame), zap.Error(err))
		return err
	} else if env == nil {
		return nil
	}
	for account := range config.Accounts {
		curErr := t.onAccountKline(account, env, bar, barExpired)
		if curErr != nil {
			if err != nil {
				log.Error("onAccountKline fail", zap.String("account", account), zap.Error(curErr))
			} else {
				err = curErr
			}
		}
	}
	return err
}

func (t *Trader) onAccountKline(account string, env *ta.BarEnv, bar *orm.InfoKline, barExpired bool) *errs.Error {
	envKey := strings.Join([]string{bar.Symbol, bar.TimeFrame}, "_")
	// 获取交易任务
	jobs, _ := strategy.GetJobs(account)[envKey]
	// 辅助订阅的任务
	infoJobs, _ := strategy.GetInfoJobs(account)[envKey]
	if len(jobs) == 0 && len(infoJobs) == 0 {
		return nil
	}
	openOds, lock := orm.GetOpenODs(account)
	// 更新非生产模式的订单
	lock.Lock()
	allOrders := utils.ValsOfMap(openOds)
	lock.Unlock()
	odMgr := GetOdMgr(account)
	var err *errs.Error
	if !core.IsWarmUp {
		// 这里可能修改订单状态
		err = odMgr.UpdateByBar(allOrders, bar)
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
		snap := job.SnapOrderStates()
		job.Stagy.OnBar(job)
		var isBatch = false
		if !barExpired {
			isBatch = job.Stagy.BatchInOut && job.Stagy.OnBatchJobs != nil
			if isBatch {
				AddBatchJob(account, bar.TimeFrame, job, false)
			} else {
				enters = append(enters, job.Entrys...)
			}
		}
		if !core.IsWarmUp {
			curEdits, err := job.CheckCustomExits(snap)
			if err != nil {
				return err
			}
			edits = append(edits, curEdits...)
		}
		if !isBatch {
			exits = append(exits, job.Exits...)
		}
	}
	// 更新辅助订阅数据
	for _, job := range infoJobs {
		job.Stagy.OnInfoBar(job, env, bar.Symbol, bar.TimeFrame)
		if job.Stagy.BatchInfo && job.Stagy.OnBatchInfos != nil {
			AddBatchJob(account, bar.TimeFrame, job, true)
		}
	}
	// 处理订单
	return t.ExecOrders(odMgr, jobs, env, enters, exits, edits)
}

func (t *Trader) ExecOrders(odMgr IOrderMgr, jobs map[string]*strategy.StagyJob, env *ta.BarEnv,
	enters []*strategy.EnterReq, exits []*strategy.ExitReq, edits []*orm.InOutEdit) *errs.Error {
	if core.IsWarmUp || len(enters)+len(exits)+len(edits) == 0 {
		return nil
	}
	var sess *orm.Queries
	var conn *pgxpool.Conn
	var err *errs.Error
	if core.EnvReal {
		// 实时模式保存到数据库。非实时模式，订单临时保存到内存，无需数据库
		sess, conn, err = orm.Conn(nil)
		if err != nil {
			log.Error("get db sess fail", zap.Error(err))
			return err
		}
		defer conn.Release()
	}
	var ents, exts []*orm.InOutOrder
	ents, exts, err = odMgr.ProcessOrders(sess, env, enters, exits, edits)
	if err != nil {
		log.Error("process orders fail", zap.Error(err))
		return err
	}
	var jobMap = map[string]*strategy.StagyJob{}
	for _, job := range jobs {
		if job.Stagy.OnOrderChange == nil {
			continue
		}
		jobMap[job.Stagy.Name] = job
	}
	for _, od := range ents {
		job, ok := jobMap[od.Strategy]
		if !ok || job.Stagy.OnOrderChange == nil {
			continue
		}
		job.Stagy.OnOrderChange(job, od, strategy.OdChgEnter)
	}
	for _, od := range exts {
		job, ok := jobMap[od.Strategy]
		if !ok || job.Stagy.OnOrderChange == nil {
			continue
		}
		job.Stagy.OnOrderChange(job, od, strategy.OdChgExit)
	}
	return nil
}

func (t *Trader) OnEnvEnd(bar *banexg.PairTFKline, adj *orm.AdjInfo) {
	mgr := GetOdMgr("")
	err := mgr.OnEnvEnd(bar, adj)
	if err != nil {
		log.Warn("close orders on env end fail", zap.Error(err))
	}
}
