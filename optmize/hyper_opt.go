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
	"github.com/c-bata/goptuna"
	"github.com/c-bata/goptuna/cmaes"
	"github.com/c-bata/goptuna/tpe"
	"github.com/d4l3k/go-bayesopt"
	"go.uber.org/zap"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

type FuncOptTask func(params map[string]float64) (float64, *errs.Error)

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
	file, err_ := os.Create(fmt.Sprintf("%s/opt_%s.log", outDir, args.Sampler))
	if err_ != nil {
		return errs.New(errs.CodeIOWriteFail, err_)
	}
	defer file.Close()
	log.Warn("running optimize jobs", zap.Int("num", len(groups)), zap.Int("rounds", args.OptRounds))
	file.WriteString(fmt.Sprintf("run hyper optimize: %v, groups: %v", args.Sampler, len(groups)))
	for _, gp := range groups {
		config.RunPolicy = []*config.RunPolicyConfig{gp}
		_ = strategy.New(gp)
		params := gp.HyperParams()
		if len(params) == 0 {
			log.Warn("no hyper params, skip optimize", zap.String("strtg", gp.ID()))
			continue
		}
		file.WriteString(fmt.Sprintf("\n============== %s =============\n", gp.ID()))
		runOptJob := func(data map[string]float64) (float64, *errs.Error) {
			score, bt, err := runOnce(gp, data)
			line := paramsToStr(data, score)
			if err != nil {
				line += fmt.Sprintf("backtest fail: %v", err)
			} else {
				line += fmt.Sprintf(" \todNum: %v, profit: %.1f%%, drawDown: %.1f%%, sharpe: %.2f\n",
					len(orm.HistODs), bt.TotProfitPct, bt.MaxDrawDownPct, bt.SharpeRatio)
			}
			file.WriteString(line)
			log.Warn(line)
			return score, nil
		}
		var best map[string]float64
		var bestSc float64
		if args.Sampler == "bayes" {
			best, bestSc, err = runBayes(args.OptRounds, params, runOptJob)
		} else {
			best, bestSc, err = runGOptuna(args.Sampler, args.OptRounds, params, runOptJob)
		}
		if err != nil {
			log.Error("optimize fail", zap.String("job", gp.ID()), zap.Error(err))
		} else {
			for _, p := range params {
				best[p.Name], _ = p.ToRegular(best[p.Name])
			}
			line := "[best] " + paramsToStr(best, bestSc)
			file.WriteString(line + "\n")
			log.Warn(line)
		}
	}
	core.RunExitCalls()
	return nil
}

func runGOptuna(name string, rounds int, params []*core.Param, loop FuncOptTask) (map[string]float64, float64, *errs.Error) {
	var sampler goptuna.Sampler
	var options []goptuna.StudyOption
	var seed = int64(0)
	if name == "random" {
		sampler = goptuna.NewRandomSampler(goptuna.RandomSamplerOptionSeed(seed))
	} else if name == "cmaes" {
		sampler = goptuna.NewRandomSampler(goptuna.RandomSamplerOptionSeed(seed))
		rs := cmaes.NewSampler(cmaes.SamplerOptionSeed(seed))
		options = append(options, goptuna.StudyOptionRelativeSampler(rs))
	} else if name == "ipop-cmaes" {
		sampler = goptuna.NewRandomSampler(goptuna.RandomSamplerOptionSeed(seed))
		rs := cmaes.NewSampler(cmaes.SamplerOptionSeed(seed),
			cmaes.SamplerOptionIPop(2))
		options = append(options, goptuna.StudyOptionRelativeSampler(rs))
	} else if name == "bipop-cmaes" {
		sampler = goptuna.NewRandomSampler(goptuna.RandomSamplerOptionSeed(seed))
		rs := cmaes.NewSampler(cmaes.SamplerOptionSeed(seed),
			cmaes.SamplerOptionBIPop(2))
		options = append(options, goptuna.StudyOptionRelativeSampler(rs))
	} else if name == "tpe" {
		sampler = tpe.NewSampler()
	} else {
		panic("invalid sampler")
	}
	options = append(options, goptuna.StudyOptionSampler(sampler))
	study, err_ := goptuna.CreateStudy("optimize", options...)
	if err_ != nil {
		return nil, 0, errs.New(errs.CodeRunTime, err_)
	}
	err_ = study.Optimize(func(trial goptuna.Trial) (float64, error) {
		var data = make(map[string]float64)
		for _, p := range params {
			minVal, maxVal := p.OptSpace()
			var val float64
			var valid bool
			for i := 0; i < 100; i++ {
				val, _ = trial.SuggestFloat(p.Name, minVal, maxVal)
				val, valid = p.ToRegular(val)
				if valid {
					break
				}
			}
			data[p.Name] = val
		}
		score, err := loop(data)
		if err != nil {
			return 0, err
		}
		return score, nil
	}, rounds)
	if err_ != nil {
		return nil, 0, errs.New(errs.CodeRunTime, err_)
	}
	best, err_ := study.GetBestParams()
	if err_ != nil {
		return nil, 0, errs.New(errs.CodeRunTime, err_)
	}
	bestSc, err_ := study.GetBestValue()
	if err_ != nil {
		return nil, 0, errs.New(errs.CodeRunTime, err_)
	}
	res := make(map[string]float64)
	for k, v := range best {
		res[k] = v.(float64)
	}
	return res, bestSc, nil
}

func runBayes(rounds int, params []*core.Param, loop FuncOptTask) (map[string]float64, float64, *errs.Error) {
	bysParams := make([]bayesopt.Param, 0, len(params))
	for _, p := range params {
		minVal, maxVal := p.OptSpace()
		bysParams = append(bysParams, bayesopt.UniformParam{
			Name: p.Name,
			Min:  minVal,
			Max:  maxVal,
		})
	}
	options := []bayesopt.OptimizerOption{
		bayesopt.WithParallel(1),
		bayesopt.WithRounds(rounds),
		bayesopt.WithRandomRounds(rounds / 3),
	}
	opt := bayesopt.New(bysParams, options...)
	best, bestSc, err_ := opt.Optimize(func(m map[bayesopt.Param]float64) float64 {
		var data = make(map[string]float64)
		for k, v := range m {
			data[k.GetName()] = v
		}
		for _, p := range params {
			data[p.Name], _ = p.ToRegular(data[p.Name])
		}
		score, _ := loop(data)
		return score
	})
	res := make(map[string]float64)
	for k, v := range best {
		res[k.GetName()] = v
	}
	if err_ != nil {
		return nil, 0, errs.New(errs.CodeRunTime, err_)
	}
	return res, bestSc, nil
}

func runOnce(gp *config.RunPolicyConfig, params map[string]float64) (float64, *BTResult, *errs.Error) {
	pol := gp.Clone()
	for k, v := range params {
		pol.Params[k] = v
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
	return -score, bt.BTResult, nil
}

func paramsToStr(m map[string]float64, score float64) string {
	var b strings.Builder
	arr := make([]*core.StrVal, 0, len(m))
	for k, v := range m {
		arr = append(arr, &core.StrVal{Str: k, Val: v})
	}
	sort.Slice(arr, func(i, j int) bool {
		return arr[i].Str < arr[j].Str
	})
	numLen := 0
	for _, p := range arr {
		valStr := strconv.FormatFloat(p.Val, 'f', 2, 64)
		b.WriteString(fmt.Sprintf("%s: %s, ", p.Str, valStr))
		numLen += len(valStr)
	}
	tabLack := (len(arr)*5 - numLen) / 4
	if tabLack > 0 {
		b.WriteString(strings.Repeat("\t", tabLack))
	}
	return fmt.Sprintf("loss: %7.2f \t%s", score, b.String())
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
