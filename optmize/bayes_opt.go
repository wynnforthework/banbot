package optmize

import (
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	ta "github.com/banbox/banta"
	"github.com/d4l3k/go-bayesopt"
	"go.uber.org/zap"
	"math"
	"os"
	"sort"
	"strings"
)

func RunOptimize(args *config.CmdArgs) *errs.Error {
	args.CPUProfile = false
	args.MemProfile = false
	args.LogLevel = "warn"
	core.SetRunMode(core.RunModeBackTest)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	// 将优化任务按多空拆分
	groups := make([]*config.RunPolicyConfig, 0, len(config.RunPolicy))
	for _, pol := range config.RunPolicy {
		if pol.Dirt == "" {
			long := pol.Clone()
			long.Dirt = "long"
			short := pol.Clone()
			short.Dirt = "short"
			groups = append(groups, long, short)
		} else {
			groups = append(groups, pol)
		}
	}
	// 针对每个策略、多空单独进行贝叶斯优化，寻找最佳参数
	err = orm.InitTask()
	if err != nil {
		return err
	}
	taskId := orm.GetTaskID("")
	outDir := fmt.Sprintf("%s/backtest/task_%d", config.GetDataDir(), taskId)
	err_ := utils.EnsureDir(outDir, 0755)
	if err_ != nil {
		return errs.New(errs.CodeIOWriteFail, err_)
	}
	file, err_ := os.Create(fmt.Sprintf("%s/optimize.log", outDir))
	if err_ != nil {
		return errs.New(errs.CodeIOWriteFail, err_)
	}
	defer file.Close()
	options := []bayesopt.OptimizerOption{
		bayesopt.WithParallel(1),
		bayesopt.WithMinimize(false),
		bayesopt.WithRounds(args.OptRounds),
		bayesopt.WithRandomRounds(args.OptRounds / 3),
	}
	log.Warn("running optimize jobs", zap.Int("num", len(groups)), zap.Int("rounds", args.OptRounds))
	for _, gp := range groups {
		config.RunPolicy = []*config.RunPolicyConfig{gp}
		strtg := strategy.New(gp)
		if len(strtg.Params) == 0 {
			log.Warn("no bayes params, skip optimize", zap.String("strtg", gp.ID()))
			continue
		}
		file.WriteString(fmt.Sprintf("\n================= %s =============\n", gp.ID()))
		opt := bayesopt.New(strtg.Params, options...)
		best, bestSc, err_ := opt.Optimize(func(m map[bayesopt.Param]float64) float64 {
			pol := gp.Clone()
			for k, v := range m {
				name := k.GetName()
				pol.Params[name] = v
			}
			config.RunPolicy = []*config.RunPolicyConfig{pol}
			core.BotRunning = true
			ResetVars()
			bt := NewBackTest()
			bt.Run()
			var score float64
			if bt.TotProfitPct <= 0 {
				score = bt.TotProfitPct
			} else {
				// 盈利时返回无回撤收益率
				score = bt.TotProfitPct * math.Pow(1-bt.MaxDrawDownPct/100, 1.5)
			}
			line := fmt.Sprintf("%s \todNum: %v, profit: %.1f%%, drawDown: %.1f%%, sharpe: %.1f\n",
				paramsToStr(m, score), len(orm.HistODs), bt.TotProfitPct, bt.MaxDrawDownPct, bt.SharpeRatio)
			file.WriteString(line)
			log.Warn(line)
			return score
		})
		if err_ != nil {
			log.Error("optimize fail", zap.String("job", gp.ID()), zap.Error(err_))
		} else {
			line := "[best] " + paramsToStr(best, bestSc)
			file.WriteString(line + "\n")
			log.Warn(line)
		}
	}
	core.RunExitCalls()
	return nil
}

func paramsToStr(m map[bayesopt.Param]float64, score float64) string {
	var b strings.Builder
	arr := make([]*core.StrVal, 0, len(m))
	for k, v := range m {
		arr = append(arr, &core.StrVal{Str: k.GetName(), Val: v})
	}
	sort.Slice(arr, func(i, j int) bool {
		return arr[i].Str < arr[j].Str
	})
	for _, p := range arr {
		b.WriteString(fmt.Sprintf("%s: %.2f  ", p.Str, p.Val))
	}
	return fmt.Sprintf("score: %.2f \t%s", score, b.String())
}

func ResetVars() {
	core.NoEnterUntil = make(map[string]int64)
	core.PairCopiedMs = make(map[string][2]int64)
	core.TfPairHits = make(map[string]map[string]int)
	biz.ResetVars()
	core.LastBarMs = 0
	core.OdBooks = make(map[string]*banexg.OrderBook)
	orm.HistODs = make([]*orm.InOutOrder, 0)
	orm.FakeOdId = 1
	orm.ResetVars()
	strategy.Envs = make(map[string]*ta.BarEnv)
	strategy.AccJobs = make(map[string]map[string]map[string]*strategy.StagyJob)
	strategy.AccInfoJobs = make(map[string]map[string]map[string]*strategy.StagyJob)
	strategy.PairStags = make(map[string]map[string]*strategy.TradeStagy)
	strategy.BatchJobs = make(map[string]map[string]*strategy.StagyJob)
	strategy.BatchInfos = make(map[string]map[string]*strategy.StagyJob)
	strategy.TFEnterMS = make(map[string]int64)
	strategy.TFInfoMS = make(map[string]int64)
	strategy.LastBatchMS = make(map[string]int64)
}
