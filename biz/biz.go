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
	"github.com/banbox/banbot/strategy"
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
	warms, err = strategy.LoadStagyJobs(pairs, pairTfScores)
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

var lockBatch = sync.Mutex{} // 防止并发修改TFEnterMS/BatchJobs

/*
AddBatchJob
添加批量入场任务。
即使job没有入场任务，也应该调用此方法，用于推迟入场时间TFEnterMS
*/
func AddBatchJob(account, tf string, job *strategy.StagyJob, isInfo bool) {
	lockBatch.Lock()
	defer lockBatch.Unlock()
	data := strategy.BatchJobs
	tsMap := strategy.TFEnterMS
	if isInfo {
		data = strategy.BatchInfos
		tsMap = strategy.TFInfoMS
	}
	key := fmt.Sprintf("%s_%s_%s", tf, account, job.Stagy.Name)
	execMS, timeOK := tsMap[tf]
	jobs, ok := data[key]
	if !ok || !timeOK || execMS < job.Env.TimeStart {
		jobs = map[string]*strategy.StagyJob{}
		data[key] = jobs
	}
	// 推迟3s等待执行
	execMS = btime.TimeMS() + core.DelayEnterMS
	tsMap[tf] = execMS
	jobs[job.Symbol.Symbol] = job
}

func TryFireEnters(tf string) {
	lockBatch.Lock()
	defer lockBatch.Unlock()
	execMS, timeOK := strategy.TFEnterMS[tf]
	if !timeOK || execMS > btime.TimeMS() {
		// 没有可执行入场的。或者有新bar推迟了执行时间
		return
	}
	var sess *orm.Queries
	var conn *pgxpool.Conn
	var err *errs.Error
	if core.EnvReal {
		// 实时模式保存到数据库。非实时模式，订单临时保存到内存，无需数据库
		sess, conn, err = orm.Conn(nil)
		if err != nil {
			log.Error("get db sess fail", zap.Error(err))
			return
		}
		defer conn.Release()
	}
	for key, jobs := range strategy.BatchJobs {
		if !strings.HasPrefix(key, tf) || len(jobs) == 0 {
			continue
		}
		delete(strategy.BatchJobs, key)
		jobList := utils.ValsOfMap(jobs)
		var stagy = jobList[0].Stagy
		// 检查此时间所有批量任务，决定哪些入场或那些出场
		stagy.OnBatchJobs(jobList)
		// 执行入场/出场任务
		keyParts := strings.Split(key, "_")
		odMgr := GetOdMgr(keyParts[1])
		var ents []*orm.InOutOrder
		var exits []*orm.InOutOrder
		for _, job := range jobs {
			if len(job.Entrys) == 0 && len(job.Exits) == 0 {
				continue
			}
			ents, exits, err = odMgr.ProcessOrders(sess, job.Env, job.Entrys, job.Exits, nil)
			if job.Stagy.OnOrderChange != nil {
				for _, od := range ents {
					job.Stagy.OnOrderChange(job, od, strategy.OdChgEnter)
				}
				for _, od := range exits {
					job.Stagy.OnOrderChange(job, od, strategy.OdChgExit)
				}
			}
			if err != nil {
				log.Error("process orders fail", zap.Error(err))
			}
		}
	}
	delete(strategy.TFEnterMS, tf)
}

func TryFireInfos(tf string) {
	lockBatch.Lock()
	defer lockBatch.Unlock()
	execMS, timeOK := strategy.TFInfoMS[tf]
	if !timeOK || execMS > btime.TimeMS() {
		// 没有可执行入场的。或者有新bar推迟了执行时间
		return
	}
	for key, jobs := range strategy.BatchInfos {
		if !strings.HasPrefix(key, tf) || len(jobs) == 0 {
			continue
		}
		delete(strategy.BatchInfos, key)
		var stagy *strategy.TradeStagy
		for _, job := range jobs {
			stagy = job.Stagy
			break
		}
		stagy.OnBatchInfos(jobs)
	}
	delete(strategy.TFInfoMS, tf)
}

func ResetVars() {
	accLiveOdMgrs = make(map[string]*LiveOrderMgr)
	accOdMgrs = make(map[string]IOrderMgr)
	accWallets = make(map[string]*BanWallets)
}
