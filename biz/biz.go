package biz

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/orm/ormo"
	"github.com/banbox/banbot/rpc"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	ta "github.com/banbox/banta"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

//go:embed config.yml
var configData []byte

//go:embed config.local.yml
var configLocalData []byte

func SetupComs(args *config.CmdArgs) *errs.Error {
	errs.PrintErr = utils.PrintErr
	ctx, cancel := context.WithCancel(context.Background())
	core.Ctx = ctx
	core.StopAll = cancel
	err := InitDataDir()
	if err != nil {
		return err
	}
	err = config.LoadConfig(args)
	if err != nil {
		return err
	}
	var logCores []zapcore.Core
	if core.LiveMode {
		logCores = append(logCores, rpc.NewExcNotify())
	}
	args.SetLog(true, logCores...)
	err = core.Setup()
	if err != nil {
		return err
	}
	err = exg.Setup()
	if err != nil {
		return err
	}
	err = orm.Setup()
	if err != nil {
		return err
	}
	err = goods.Setup()
	if err != nil {
		return err
	}
	return nil
}

func SetupComsExg(args *config.CmdArgs) *errs.Error {
	err := SetupComs(args)
	if err != nil {
		return err
	}
	return orm.InitExg(exg.Default)
}

func LoadRefreshPairs(dp data.IProvider, showLog bool) *errs.Error {
	goods.ShowLog = showLog
	pairs, err := goods.RefreshPairList()
	if err != nil {
		return err
	}
	pairTfScores, err := calcPairTfScales(exg.Default, pairs)
	if err != nil {
		return err
	}
	warms, accOds, err := strat.LoadStratJobs(pairs, pairTfScores)
	if err != nil {
		return err
	}
	if len(accOds) > 0 {
		// exit unfill orders
		var sess *ormo.Queries
		if core.LiveMode {
			sess, err = ormo.Conn(orm.DbTrades, true)
			if err != nil {
				return err
			}
		}
		for acc, odList := range accOds {
			odMgr := GetOdMgr(acc)
			for _, od := range odList {
				req := &strat.ExitReq{Tag: core.ExitTagPairDel, OrderID: od.ID, ExitRate: 1}
				_, err = odMgr.ExitOrder(sess, od, req)
				if err != nil {
					return err
				}
			}
		}
	}
	if showLog {
		core.PrintStratGroups()
	}
	return dp.SubWarmPairs(warms, true)
}

func AutoRefreshPairs(dp data.IProvider, showLog bool) {
	err := LoadRefreshPairs(dp, showLog)
	if err != nil {
		log.Error("refresh pairs fail", zap.Error(err))
	}
}

func InitOdSubs() {
	var subStgys = map[string]*strat.TradeStrat{}
	for _, items := range strat.PairStrats {
		for stgName, stagy := range items {
			if stagy.OnOrderChange != nil {
				subStgys[stgName] = stagy
			}
		}
	}
	if len(subStgys) == 0 {
		return
	}
	for acc := range strat.AccJobs {
		strat.AddOdSub(acc, func(acc string, od *ormo.InOutOrder, evt int) {
			stgy, ok := subStgys[od.Strategy]
			if !ok {
				// The current strategy does not monitor order status
				// 当前策略未监听订单状态
				return
			}
			items, _ := strat.AccJobs[acc]
			if len(items) == 0 {
				return
			}
			pairTF := strings.Join([]string{od.Symbol, od.Timeframe}, "_")
			its, _ := items[pairTF]
			if len(its) == 0 {
				return
			}
			job, _ := its[od.Strategy]
			if job != nil {
				stgy.OnOrderChange(job, od, evt)
			}
		})
	}
}

// Preventing Concurrent Modification of BatchTasks
var lockBatch = sync.Mutex{} // 防止并发修改BatchTasks

/*
AddBatchJob
Add batch entry tasks.
Even if the job has no entry tasks, this method should be called to postpone the entry time TFEnterMS
添加批量入场任务。
即使job没有入场任务，也应该调用此方法，用于推迟入场时间TFEnterMS
*/
func AddBatchJob(account, tf string, job *strat.StratJob, isInfo bool) {
	lockBatch.Lock()
	defer lockBatch.Unlock()
	key := fmt.Sprintf("%s_%s_%s", tf, account, job.Strat.Name)
	tasks, ok := strat.BatchTasks[key]
	if !ok {
		tasks = &strat.BatchMap{
			Map:     make(map[string]*strat.BatchTask),
			TFMSecs: int64(utils2.TFToSecs(tf) * 1000),
		}
		strat.BatchTasks[key] = tasks
	}
	// Delay 3s to wait for execution
	// 推迟3s等待执行
	tasks.ExecMS = btime.TimeMS() + core.DelayBatchMS
	var batchType = strat.BatchTypeInOut
	if isInfo {
		batchType = strat.BatchTypeInfo
	}
	tasks.Map[job.Symbol.Symbol] = &strat.BatchTask{Job: job, Type: batchType}
}

func TryFireBatches(currMS int64) int {
	lockBatch.Lock()
	defer lockBatch.Unlock()
	var sess *ormo.Queries
	var err *errs.Error
	if core.EnvReal {
		// In real-time mode, the order is saved to the database. In non-real-time mode, the order is temporarily saved to the memory without the need for a database.
		// 实时模式保存到数据库。非实时模式，订单临时保存到内存，无需数据库
		sess, err = ormo.Conn(orm.DbTrades, true)
		if err != nil {
			log.Error("get db sess fail", zap.Error(err))
			return 0
		}
	}
	var waitNum = 0
	for key, tasks := range strat.BatchTasks {
		if currMS < tasks.ExecMS {
			if tasks.ExecMS-currMS < tasks.TFMSecs/2 {
				// Batch processing time has not yet arrived
				// 尚未到达批量处理时间
				waitNum += 1
			}
			continue
		}
		var enterJobs []*strat.StratJob
		var infoJobs = make(map[string]*strat.StratJob)
		var stgy *strat.TradeStrat
		for pair, task := range tasks.Map {
			stgy = task.Job.Strat
			if task.Type == strat.BatchTypeInOut {
				enterJobs = append(enterJobs, task.Job)
			} else if task.Type == strat.BatchTypeInfo {
				infoJobs[pair] = task.Job
			} else {
				panic(fmt.Sprintf("unsupport BatchType: %v", task.Type))
			}
		}
		delete(strat.BatchTasks, key)
		if len(enterJobs) > 0 {
			// Check all batch tasks at this time and decide which ones to enter or exit
			// 检查此时间所有批量任务，决定哪些入场或那些出场
			stgy.OnBatchJobs(enterJobs)
			// Perform entry/exit tasks
			// 执行入场/出场任务
			keyParts := strings.Split(key, "_")
			odMgr := GetOdMgr(keyParts[1])
			var ents []*ormo.InOutOrder
			var exits []*ormo.InOutOrder
			for _, job := range enterJobs {
				if len(job.Entrys) == 0 && len(job.Exits) == 0 {
					continue
				}
				ents, exits, err = odMgr.ProcessOrders(sess, job.Env, job.Entrys, job.Exits, nil)
				if job.Strat.OnOrderChange != nil {
					for _, od := range ents {
						job.Strat.OnOrderChange(job, od, strat.OdChgEnter)
					}
					for _, od := range exits {
						job.Strat.OnOrderChange(job, od, strat.OdChgExit)
					}
				}
				if err != nil {
					log.Error("process orders fail", zap.Error(err))
				}
			}
		}
		if len(infoJobs) > 0 {
			stgy.OnBatchInfos(infoJobs)
		}
	}
	return waitNum
}

func ResetVars() {
	core.NoEnterUntil = make(map[string]int64)
	core.PairCopiedMs = make(map[string][2]int64)
	core.TfPairHits = make(map[string]map[string]int)
	accLiveOdMgrs = make(map[string]*LiveOrderMgr)
	accOdMgrs = make(map[string]IOrderMgr)
	accWallets = make(map[string]*BanWallets)
	core.LastBarMs = 0
	core.OdBooks = make(map[string]*banexg.OrderBook)
	ormo.HistODs = make([]*ormo.InOutOrder, 0)
	//orm.FakeOdId = 1
	ormo.ResetVars()
	strat.Envs = make(map[string]*ta.BarEnv)
	strat.AccJobs = make(map[string]map[string]map[string]*strat.StratJob)
	strat.AccInfoJobs = make(map[string]map[string]map[string]*strat.StratJob)
	strat.PairStrats = make(map[string]map[string]*strat.TradeStrat)
	strat.BatchTasks = make(map[string]*strat.BatchMap)
	strat.LastBatchMS = 0
}

func replaceDockerHosts(data []byte) []byte {
	if !utils.IsDocker() {
		return data
	}
	content := string(data)
	content = strings.ReplaceAll(content, "127.0.0.1", "host.docker.internal")
	content = strings.ReplaceAll(content, "localhost", "host.docker.internal")
	return []byte(content)
}

func InitDataDir() *errs.Error {
	dataDir := config.GetDataDir()
	err_ := utils.EnsureDir(dataDir, 0755)
	if dataDir == "" {
		return errs.NewMsg(errs.CodeParamRequired, "-datadir or env `BanDataDir` is required")
	}
	if err_ != nil {
		return errs.New(errs.CodeIOWriteFail, err_)
	}
	var err *errs.Error
	configPath := filepath.Join(dataDir, "config.yml")
	configLocalPath := filepath.Join(dataDir, "config.local.yml")
	exists := utils.Exists(configPath)
	existLocal := utils.Exists(configLocalPath)
	if exists || existLocal {
		// dont init config in dataDir if any of config.yml/config.local.yml exist
		return nil
	}
	if !exists {
		err = utils.WriteFile(configPath, replaceDockerHosts(configData))
		log.Info("init done: $/config.yml")
	}
	if !existLocal {
		err = utils.WriteFile(configLocalPath, replaceDockerHosts(configLocalData))
		log.Info("init done: $/config.local.yml")
	}
	return err
}
