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
	log.Setup(config.Args.Debug, config.Args.Logfile, logCores...)
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
AddJobEnters
添加批量入场任务。
即使job没有入场任务，也应该调用此方法，用于推迟入场时间TFEnterMS
*/
func AddJobEnters(account, tf string, job *strategy.StagyJob) {
	lockBatch.Lock()
	defer lockBatch.Unlock()
	key := fmt.Sprintf("%s_%s_%s", tf, account, job.Stagy.Name)
	execMS, timeOK := strategy.TFEnterMS[tf]
	jobs, ok := strategy.BatchJobs[key]
	if !ok || !timeOK || execMS < job.Env.TimeStart {
		jobs = map[string]*strategy.StagyJob{}
		strategy.BatchJobs[key] = jobs
	}
	// 推迟3s等待执行
	execMS = btime.TimeMS() + core.DelayEnterMS
	strategy.TFEnterMS[tf] = execMS
	if len(job.Entrys) > 0 {
		jobs[job.Symbol.Symbol] = job
	} else {
		jobs[job.Symbol.Symbol] = nil
	}
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
	if core.LiveMode {
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
		var stagy *strategy.TradeStagy
		var entJobs = make([]*strategy.StagyJob, 0, len(jobs)/5)
		var noPairs = make([]string, 0, len(jobs))
		for pair, job := range jobs {
			if job == nil {
				noPairs = append(noPairs, pair)
				continue
			}
			entJobs = append(entJobs, job)
			if stagy == nil {
				stagy = job.Stagy
			}
		}
		if len(entJobs) == 0 {
			continue
		}
		// 检查此时间所有入场任务，过滤，返回允许入场的任务
		entJobs = stagy.OnBatchJobs(entJobs, noPairs)
		if len(entJobs) == 0 {
			continue
		}
		// 执行入场任务
		keyParts := strings.Split(key, "_")
		odMgr := GetOdMgr(keyParts[1])
		var ents []*orm.InOutOrder
		for _, job := range entJobs {
			if len(job.Entrys) == 0 {
				continue
			}
			ents, _, err = odMgr.ProcessOrders(sess, job.Env, job.Entrys, nil, nil)
			if len(ents) > 0 && job.Stagy.OnOrderChange != nil {
				for _, od := range ents {
					job.Stagy.OnOrderChange(job, od, strategy.OdChgEnter)
				}
			}
			if err != nil {
				log.Error("process orders fail", zap.Error(err))
			}
		}
	}
	delete(strategy.TFEnterMS, tf)
}
