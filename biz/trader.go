package biz

import (
	"database/sql"
	"fmt"
	"math"
	"strings"

	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/orm/ormo"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	ta "github.com/banbox/banta"
	"go.uber.org/zap"
)

type Trader struct {
}

func (t *Trader) OnEnvJobs(bar *orm.InfoKline) (*ta.BarEnv, *errs.Error) {
	envKey := strings.Join([]string{bar.Symbol, bar.TimeFrame}, "_")
	env, ok := strat.Envs[envKey]
	if !ok {
		// 额外订阅1h没有对应的env，无需处理
		return nil, nil
	}
	if core.LiveMode {
		if env.TimeStop > bar.Time {
			// This bar has expired, ignore it, the crawler may push the processed expired bar when starting
			// 此bar已过期，忽略，启动时爬虫可能会推已处理的过期bar
			return nil, nil
		} else if env.TimeStop > 0 && env.TimeStop < bar.Time {
			lackNum := int(math.Round(float64(bar.Time-env.TimeStop) / float64(env.TFMSecs)))
			if lackNum > 0 {
				log.Warn("taEnv bar lack", zap.Int("num", lackNum), zap.String("env", envKey))
			}
		}
	}
	// Update BarEnv status
	// 更新BarEnv状态
	err := env.OnBar(bar.Time, bar.Open, bar.High, bar.Low, bar.Close, bar.Volume, bar.Info)
	if err != nil {
		return nil, errs.New(errs.CodeRunTime, err)
	}
	return env, nil
}

func (t *Trader) FeedKline(bar *orm.InfoKline) *errs.Error {
	tfSecs := utils2.TFToSecs(bar.TimeFrame)
	core.SetBarPrice(bar.Symbol, bar.Close)
	// If it exceeds 1 minute and half of the period, the bar is considered delayed and orders cannot be placed.
	// 超过1分钟且周期的一半，认为bar延迟，不可下单
	delaySecs := int((btime.TimeMS()-bar.Time)/1000) - tfSecs
	barExpired := delaySecs >= max(60, tfSecs/2)
	if barExpired && core.LiveMode && !bar.IsWarmUp {
		log.Warn(fmt.Sprintf("%s/%s delay %v s, open order is disabled", bar.Symbol, bar.TimeFrame, delaySecs))
	}
	// Update indicator environment
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
	// Get strategy jobs 获取交易任务
	jobs, _ := strat.GetJobs(account)[envKey]
	// jobs which subscript info timeframes  辅助订阅的任务
	infoJobs, _ := strat.GetInfoJobs(account)[envKey]
	if len(jobs) == 0 && len(infoJobs) == 0 {
		return nil
	}
	openOds, lock := ormo.GetOpenODs(account)
	// Update orders in non-production mode 更新非生产模式的订单
	lock.Lock()
	allOrders := utils.ValsOfMap(openOds)
	lock.Unlock()
	odMgr := GetOdMgr(account)
	var err *errs.Error
	if !bar.IsWarmUp && len(allOrders) > 0 {
		// The order status may be modified here
		// 这里可能修改订单状态
		err = odMgr.UpdateByBar(allOrders, bar)
		if err != nil {
			return err
		}
	}
	// retrieve the current open orders after UpdateByBar, filter for closed orders
	// 要在UpdateByBar后检索当前开放订单，过滤已平仓订单
	var curOrders []*ormo.InOutOrder
	for _, od := range allOrders {
		if od.Status < ormo.InOutStatusFullExit && od.Symbol == bar.Symbol && od.Timeframe == bar.TimeFrame {
			curOrders = append(curOrders, od)
		}
	}
	var enters []*strat.EnterReq
	var exits []*strat.ExitReq
	var edits []*ormo.InOutEdit
	for _, job := range jobs {
		job.IsWarmUp = bar.IsWarmUp
		job.InitBar(curOrders)
		snap := job.SnapOrderStates()
		job.Strat.OnBar(job)
		var isBatch = false
		if !barExpired {
			isBatch = job.Strat.BatchInOut && job.Strat.OnBatchJobs != nil
			if isBatch {
				AddBatchJob(account, bar.TimeFrame, job, false)
			} else {
				enters = append(enters, job.Entrys...)
			}
		}
		if !bar.IsWarmUp {
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
	// invoke OnInfoBar
	// 更新辅助订阅数据
	// 此处不应允许开平仓或更新止盈止损等，否则订单的TimeFrame会出现歧义
	for _, job := range infoJobs {
		job.IsWarmUp = bar.IsWarmUp
		job.Strat.OnInfoBar(job, env, bar.Symbol, bar.TimeFrame)
		if job.Strat.BatchInfo && job.Strat.OnBatchInfos != nil {
			AddBatchJob(account, bar.TimeFrame, job, true)
		}
	}
	// 处理订单
	if bar.IsWarmUp {
		return nil
	}
	return t.ExecOrders(odMgr, jobs, env, enters, exits, edits)
}

func (t *Trader) ExecOrders(odMgr IOrderMgr, jobs map[string]*strat.StratJob, env *ta.BarEnv,
	enters []*strat.EnterReq, exits []*strat.ExitReq, edits []*ormo.InOutEdit) *errs.Error {
	if len(enters)+len(exits)+len(edits) == 0 {
		return nil
	}
	var sess *ormo.Queries
	var conn *sql.DB
	var err *errs.Error
	if core.LiveMode {
		// Live mode is saved to the database. Non-real-time mode, orders are temporarily saved in memory, no database required
		// 实时模式保存到数据库。非实时模式，订单临时保存到内存，无需数据库
		sess, conn, err = ormo.Conn(orm.DbTrades, true)
		if err != nil {
			log.Error("get db sess fail", zap.Error(err))
			return err
		}
		defer conn.Close()
	}
	var ents, exts []*ormo.InOutOrder
	ents, exts, err = odMgr.ProcessOrders(sess, env, enters, exits, edits)
	if err != nil {
		log.Error("process orders fail", zap.Error(err))
		return err
	}
	var jobMap = map[string]*strat.StratJob{}
	for _, job := range jobs {
		if job.Strat.OnOrderChange == nil {
			continue
		}
		jobMap[job.Strat.Name] = job
	}
	for _, od := range ents {
		job, ok := jobMap[od.Strategy]
		if !ok || job.Strat.OnOrderChange == nil {
			continue
		}
		job.Strat.OnOrderChange(job, od, strat.OdChgEnter)
	}
	for _, od := range exts {
		job, ok := jobMap[od.Strategy]
		if !ok || job.Strat.OnOrderChange == nil {
			continue
		}
		job.Strat.OnOrderChange(job, od, strat.OdChgExit)
	}
	return nil
}

func (t *Trader) OnEnvEnd(bar *banexg.PairTFKline, adj *orm.AdjInfo) {
	mgrs := GetAllOdMgr()
	for acc, mgr := range mgrs {
		err := mgr.OnEnvEnd(bar, adj)
		if err != nil {
			log.Warn("close orders on env end fail", zap.String("acc", acc), zap.Error(err))
		}
	}
	envKey := strings.Join([]string{bar.Symbol, bar.TimeFrame}, "_")
	env, ok := strat.Envs[envKey]
	if ok {
		env.Reset()
	}
}
