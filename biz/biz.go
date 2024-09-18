package biz

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/rpc"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"strings"
	"sync"
)

func SetupComs(args *config.CmdArgs) *errs.Error {
	errs.PrintErr = utils.PrintErr
	ctx, cancel := context.WithCancel(context.Background())
	core.Ctx = ctx
	core.StopAll = cancel
	err := config.LoadConfig(args)
	if err != nil {
		return err
	}
	var logCores []zapcore.Core
	if core.LiveMode {
		logCores = append(logCores, rpc.NewExcNotify())
	}
	log.Setup(args.LogLevel, args.Logfile, logCores...)
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

func LoadRefreshPairs() *errs.Error {
	pairs, err := goods.RefreshPairList()
	if err != nil {
		return err
	}
	pairTfScores, err := calcPairTfScales(exg.Default, pairs)
	if err != nil {
		return err
	}
	var warms map[string]map[string]int
	warms, err = strat.LoadStagyJobs(pairs, pairTfScores)
	if err != nil {
		return err
	}
	core.PrintStagyGroups()
	return data.Main.SubWarmPairs(warms, true)
}

func AutoRefreshPairs() {
	err := LoadRefreshPairs()
	if err != nil {
		log.Error("refresh pairs fail", zap.Error(err))
	}
}

func InitOdSubs() {
	var subStagys = map[string]*strat.TradeStagy{}
	for _, items := range strat.PairStags {
		for stgName, stagy := range items {
			if stagy.OnOrderChange != nil {
				subStagys[stgName] = stagy
			}
		}
	}
	if len(subStagys) == 0 {
		return
	}
	for acc := range strat.AccJobs {
		strat.AddOdSub(acc, func(acc string, od *orm.InOutOrder, evt int) {
			stagy, ok := subStagys[od.Strategy]
			if !ok {
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
				stagy.OnOrderChange(job, od, evt)
			}
		})
	}
}

var lockBatch = sync.Mutex{} // 防止并发修改BatchTasks

/*
AddBatchJob
添加批量入场任务。
即使job没有入场任务，也应该调用此方法，用于推迟入场时间TFEnterMS
*/
func AddBatchJob(account, tf string, job *strat.StagyJob, isInfo bool) {
	lockBatch.Lock()
	defer lockBatch.Unlock()
	key := fmt.Sprintf("%s_%s_%s", tf, account, job.Stagy.Name)
	tasks, ok := strat.BatchTasks[key]
	if !ok {
		tasks = &strat.BatchMap{
			Map:     make(map[string]*strat.BatchTask),
			TFMSecs: int64(utils.TFToSecs(tf) * 1000),
		}
		strat.BatchTasks[key] = tasks
	}
	// 推迟3s等待执行
	tasks.ExecMS = btime.TimeMS() + core.DelayBatchMS
	var batchType = strat.BatchTypeEnter
	if isInfo {
		batchType = strat.BatchTypeInfo
	}
	tasks.Map[job.Symbol.Symbol] = &strat.BatchTask{Job: job, Type: batchType}
}

func TryFireBatches(currMS int64) int {
	lockBatch.Lock()
	defer lockBatch.Unlock()
	var sess *orm.Queries
	var conn *pgxpool.Conn
	var err *errs.Error
	if core.EnvReal {
		// 实时模式保存到数据库。非实时模式，订单临时保存到内存，无需数据库
		sess, conn, err = orm.Conn(nil)
		if err != nil {
			log.Error("get db sess fail", zap.Error(err))
			return 0
		}
		defer conn.Release()
	}
	var waitNum = 0
	for key, tasks := range strat.BatchTasks {
		if currMS < tasks.ExecMS {
			if tasks.ExecMS-currMS < tasks.TFMSecs/2 {
				// 尚未到达批量处理时间
				waitNum += 1
			}
			continue
		}
		var enterJobs []*strat.StagyJob
		var infoJobs map[string]*strat.StagyJob
		var stagy *strat.TradeStagy
		for pair, task := range tasks.Map {
			stagy = task.Job.Stagy
			if task.Type == strat.BatchTypeEnter {
				enterJobs = append(enterJobs, task.Job)
			} else if task.Type == strat.BatchTypeInfo {
				infoJobs[pair] = task.Job
			} else {
				panic(fmt.Sprintf("unsupport BatchType: %v", task.Type))
			}
		}
		delete(strat.BatchTasks, key)
		if len(enterJobs) > 0 {
			// 检查此时间所有批量任务，决定哪些入场或那些出场
			stagy.OnBatchJobs(enterJobs)
			// 执行入场/出场任务
			keyParts := strings.Split(key, "_")
			odMgr := GetOdMgr(keyParts[1])
			var ents []*orm.InOutOrder
			var exits []*orm.InOutOrder
			for _, job := range enterJobs {
				if len(job.Entrys) == 0 && len(job.Exits) == 0 {
					continue
				}
				ents, exits, err = odMgr.ProcessOrders(sess, job.Env, job.Entrys, job.Exits, nil)
				if job.Stagy.OnOrderChange != nil {
					for _, od := range ents {
						job.Stagy.OnOrderChange(job, od, strat.OdChgEnter)
					}
					for _, od := range exits {
						job.Stagy.OnOrderChange(job, od, strat.OdChgExit)
					}
				}
				if err != nil {
					log.Error("process orders fail", zap.Error(err))
				}
			}
		}
		if len(infoJobs) > 0 {
			stagy.OnBatchInfos(infoJobs)
		}
	}
	return waitNum
}

func ResetVars() {
	accLiveOdMgrs = make(map[string]*LiveOrderMgr)
	accOdMgrs = make(map[string]IOrderMgr)
	accWallets = make(map[string]*BanWallets)
}
