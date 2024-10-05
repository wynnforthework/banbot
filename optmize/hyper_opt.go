package optmize

import (
	"bytes"
	"fmt"
	"github.com/anyongjin/go-bayesopt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/c-bata/goptuna"
	"github.com/c-bata/goptuna/cmaes"
	"github.com/c-bata/goptuna/tpe"
	"github.com/mitchellh/mapstructure"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"io/fs"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type FuncOptTask func(params map[string]float64) (float64, *errs.Error)

/*
RunBTOverOpt
Backtesting mode based on continuous parameter tuning. Approach the real situation and avoid using future information to adjust parameters for backtesting.
基于持续调参的回测模式。接近实盘情况，避免使用未来信息调参回测。
*/
func RunBTOverOpt(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeBackTest)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	err = orm.InitExg(exg.Default)
	if err != nil {
		return err
	}
	dateRange := config.TimeRange
	allStartMs, allEndMs := dateRange.StartMS, dateRange.EndMS
	runMSecs := int64(utils.TFToSecs(args.RunPeriod)) * 1000
	reviewMSecs := int64(utils.TFToSecs(args.ReviewPeriod)) * 1000
	if runMSecs < core.SecsHour*1000 {
		log.Warn("`run-period` cannot be less than 1 hour")
		return nil
	}
	outDir := filepath.Join(config.GetDataDir(), "backtest", "bt_opt_"+btOptHash(args))
	err_ := utils.EnsureDir(outDir, 0755)
	if err_ != nil {
		return errs.New(errs.CodeIOWriteFail, err_)
	}
	log.Info("write bt over opt to", zap.String("dir", outDir))
	args.OutPath = filepath.Join(outDir, "opt.log")
	curMs := allStartMs + reviewMSecs
	var allHisOds []*orm.InOutOrder
	var lastWal map[string]float64
	var lastRes *BTResult
	initPols := config.RunPolicy
	lastPols := config.RunPolicy
	pbar := utils.NewPrgBar(int((allEndMs-curMs)/1000), "BtOpt")
	defer pbar.Close()
	for curMs < allEndMs {
		pbar.Add(int(runMSecs / 1000))
		dateRange.StartMS = curMs - reviewMSecs
		dateRange.EndMS = curMs
		fname := fmt.Sprintf("opt_%v.log", dateRange.StartMS/1000)
		args.OutPath = filepath.Join(outDir, fname)
		var polStr string
		polStr, err = pickFromExists(args.OutPath, args.Picker)
		if err != nil {
			return err
		}
		if polStr == "" {
			config.RunPolicy = initPols
			polStr, err = runOptimize(args, 11)
			if err != nil {
				return err
			}
		}
		biz.ResetVars()
		var unpak = make(map[string]interface{})
		err_ = yaml.Unmarshal([]byte(polStr), &unpak)
		if err_ != nil {
			return errs.New(errs.CodeRunTime, err_)
		}
		var cfg config.Config
		err_ = mapstructure.Decode(unpak, &cfg)
		if err_ != nil {
			return errs.New(errs.CodeRunTime, err_)
		}
		if len(cfg.RunPolicy) == 0 {
			log.Warn("no RunPolicy for ", zap.Int64("start", dateRange.StartMS/1000),
				zap.Int64("end", curMs/1000))
			curMs += runMSecs
			continue
		}
		applyOptPolicies(lastPols, cfg.RunPolicy, args.Alpha)
		lastPols = config.RunPolicy
		wallets := biz.GetWallets("")
		core.BotRunning = true
		dateRange.StartMS = curMs
		dateRange.EndMS = curMs + runMSecs
		bt := NewBackTest(false)
		if lastWal != nil {
			wallets.SetWallets(lastWal)
		}
		if lastRes != nil {
			bt.BTResult = lastRes
		}
		orm.HistODs = allHisOds
		bt.Run()
		lastRes = bt.BTResult
		allHisOds = orm.HistODs
		lastWal = wallets.DumpAvas()
		curMs += runMSecs
	}
	return nil
}

func btOptHash(args *config.CmdArgs) string {
	raws := []string{
		args.Sampler,
		strconv.FormatBool(args.EachPairs),
	}
	ymlData, err_ := config.DumpYaml()
	if ymlData != nil {
		raws = append(raws, string(ymlData))
	} else {
		log.Warn("dump config yaml fail", zap.Error(err_))
	}
	for _, p := range config.RunPolicy {
		raws = append(raws, p.Key())
	}
	res := utils.MD5([]byte(strings.Join(raws, "")))
	return res[:10]
}

func pickFromExists(path string, picker string) (string, *errs.Error) {
	paths, err_ := utils.GetFilesWithPrefix(path)
	if err_ != nil {
		return "", errs.New(errs.CodeIOReadFail, err_)
	}
	if len(paths) == 0 {
		return "", nil
	}
	return collectOptLog(paths, 11, picker)
}

/*
applyOptPolicies
Update strategy group parameters using EMA to avoid significant differences in parameters before and after rolling backtesting
使用EMA更新策略组参数，避免滚动回测前后参数差异较大
*/
func applyOptPolicies(olds, pols []*config.RunPolicyConfig, alpha float64) {
	if alpha >= 1 {
		config.RunPolicy = pols
		return
	}
	var data = make(map[string]*config.RunPolicyConfig)
	for _, p := range olds {
		data[p.Key()] = p
	}
	var res = make([]*config.RunPolicyConfig, 0, len(pols))
	for _, p := range pols {
		key := p.Key()
		old, _ := data[key]
		if old == nil {
			log.Warn("no match old", zap.String("for", key))
			res = append(res, p)
		} else {
			item := p.Clone()
			for k, v := range item.Params {
				oldV, ok := old.Params[k]
				if ok {
					item.Params[k] = v*alpha + oldV*(1-alpha)
				}
			}
			res = append(res, item)
		}
	}
	config.RunPolicy = res
}

func RunOptimize(args *config.CmdArgs) *errs.Error {
	if args.OutPath == "" {
		log.Warn("-out is required")
		return nil
	}
	args.NoDb = true
	args.CPUProfile = false
	args.MemProfile = false
	args.LogLevel = "warn"
	core.SetRunMode(core.RunModeBackTest)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	cfgStr, err := runOptimize(args, 0)
	if err != nil {
		return err
	}
	fmt.Print(cfgStr)
	return nil
}

func runOptimize(args *config.CmdArgs, minScore float64) (string, *errs.Error) {
	var err *errs.Error
	btime.CurTimeMS = config.TimeRange.StartMS
	// 列举所有标的
	allPairs := config.Pairs
	if len(allPairs) == 0 {
		goods.ShowLog = false
		allPairs, err = goods.RefreshPairList()
		if err != nil {
			return "", err
		}
	}
	var logOuts []string
	groups := config.RunPolicy
	if len(groups) <= 1 || args.Concur <= 1 {
		logOuts = append(logOuts, args.OutPath)
		file, err_ := os.Create(args.OutPath)
		if err_ != nil {
			return "", errs.New(errs.CodeIOWriteFail, err_)
		}
		defer file.Close()
		for _, gp := range groups {
			// Bayesian optimization is carried out separately for each strategy, long and short, to find the best parameters
			// 针对每个策略、多空单独进行贝叶斯优化，寻找最佳参数
			err = optAndPrint(gp.Clone(), args, allPairs, file)
			if err != nil {
				return "", err
			}
		}
	} else {
		// Multi-process execution to improve speed.
		// 多进程执行，提高速度。
		log.Warn("running optimize", zap.Int("num", len(groups)), zap.Int("rounds", args.OptRounds))
		var cmds = []string{"optimize", "--nodb", "-opt-rounds"}
		cmds = append(cmds, strconv.Itoa(args.OptRounds), "-sampler", args.Sampler)
		if args.EachPairs {
			cmds = append(cmds, "-each-pairs")
		}
		for _, p := range args.Configs {
			cmds = append(cmds, "-config", p)
		}
		if args.Picker != "" {
			cmds = append(cmds, "-picker", args.Picker)
		}
		startStr := strconv.FormatInt(config.TimeRange.StartMS/1000, 10)
		endStr := strconv.FormatInt(config.TimeRange.EndMS/1000, 10)
		err = utils.ParallelRun(groups, args.Concur, func(i int, pol *config.RunPolicyConfig) *errs.Error {
			time.Sleep(time.Millisecond * time.Duration(1000*rand.Float64()+100*float64(i)))
			iStr := strconv.Itoa(i + 1)
			cfgFile, err_ := os.CreateTemp("", "ban_opt"+iStr)
			if err_ != nil {
				log.Warn("write temp config fail", zap.Error(err_))
				return nil
			}
			defer os.Remove(cfgFile.Name())
			cfgFile.WriteString(fmt.Sprintf("timerange: \"%s-%s\"\n", startStr, endStr))
			cfgFile.WriteString("run_policy:\n")
			cfgFile.WriteString(pol.ToYaml())
			cfgFile.Close()
			curCmds := append(cmds, "-config", cfgFile.Name())
			outPath := args.OutPath + "." + iStr
			curCmds = append(curCmds, "-out", outPath)
			logOuts = append(logOuts, outPath)
			log.Warn("runing: " + strings.Join(curCmds, " "))
			var out bytes.Buffer
			prgName := "banbot.o"
			if runtime.GOOS == "windows" {
				prgName = "banbot.exe"
			}
			excPath := filepath.Join(config.GetStratDir(), prgName)
			if _, err_ = os.Stat(excPath); err_ != nil {
				return errs.New(errs.CodeRunTime, err_)
			}
			cmd := exec.Command(excPath, curCmds...)
			cmd.Dir = config.GetStratDir()
			cmd.Stdout = &out
			cmd.Stderr = &out
			err_ = cmd.Run()
			fmt.Println(out.String())
			if err_ != nil {
				return errs.New(errs.CodeRunTime, err_)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return collectOptLog(logOuts, minScore, args.Picker)
}

/*
optAndPrint optimize for raw policy group;
write one or multiple optimize result to file.
*/
func optAndPrint(pol *config.RunPolicyConfig, args *config.CmdArgs, allPairs []string, file *os.File) *errs.Error {
	file.WriteString(fmt.Sprintf("# run hyper optimize: %v, rounds: %v\n", args.Sampler, args.OptRounds))
	startDt := btime.ToDateStr(config.TimeRange.StartMS, "")
	endDt := btime.ToDateStr(config.TimeRange.EndMS, "")
	file.WriteString(fmt.Sprintf("# date range: %v - %v\n", startDt, endDt))
	var res []*GroupScore
	if args.EachPairs {
		pairs := pol.Pairs
		if len(pairs) == 0 {
			pairs = allPairs
		}
		res = make([]*GroupScore, 0, len(pairs))
		for _, p := range pairs {
			pol.Pairs = []string{p}
			item := optForGroup(pol, args.Sampler, args.Picker, args.OptRounds, file)
			if item != nil {
				res = append(res, item)
			}
		}
		sort.Slice(res, func(i, j int) bool {
			return res[i].Score > res[j].Score
		})
	} else {
		item := optForGroup(pol, args.Sampler, args.Picker, args.OptRounds, file)
		if item != nil {
			res = append(res, item)
		}
	}
	for _, gp := range res {
		if gp.Score <= 0 {
			break
		}
		file.WriteString(fmt.Sprintf("\n  # score: %.2f\n", gp.Score))
		for _, p := range gp.Items {
			file.WriteString(p.ToYaml())
		}
	}
	core.RunExitCalls()
	return nil
}

/*
optForGroup
Optimize the hyperparameters of a policy and automatically search for the best combination of long, short, and both.
对某个策略超参数调优，自动搜索long/short/both的最佳组合。
*/
func optForGroup(pol *config.RunPolicyConfig, method, picker string, rounds int, flog *os.File) *GroupScore {
	groups := make([]*config.RunPolicyConfig, 0, 3)
	var long, short, both *config.RunPolicyConfig
	if pol.Dirt == "any" {
		long = pol.Clone()
		long.Dirt = "long"
		short = pol.Clone()
		short.Dirt = "short"
		both = pol.Clone()
		both.Dirt = ""
		groups = append(groups, long, short, both)
	} else {
		groups = append(groups, pol.Clone())
	}
	var bestOdNum = 0
	var bestScore = -999.0
	var bestPols []*config.RunPolicyConfig
	for _, p := range groups {
		config.RunPolicy = []*config.RunPolicyConfig{p}
		optForPol(p, method, picker, rounds, flog)
		if p.Score > bestScore {
			bestOdNum = p.MaxOpen
			bestScore = p.Score
			bestPols = []*config.RunPolicyConfig{p}
		}
	}
	if len(groups) == 1 {
		return &GroupScore{groups, bestScore}
	}
	// long,short,both分别评估
	minScore := min(long.Score, short.Score)
	maxScore := max(long.Score, short.Score)
	if minScore > 0 && maxScore > 0 {
		// 检查组合的是否优于long/short/both
		flog.WriteString("\n========== union long/short ============\n")
		config.RunPolicy = []*config.RunPolicyConfig{long, short}
		bt, loss := runBTOnce()
		line := fmt.Sprintf("loss: %5.2f \todNum: %v, profit: %.1f%%, drawDown: %.1f%%, sharpe: %.2f\n",
			loss, bt.OrderNum, bt.TotProfitPct, bt.MaxDrawDownPct, bt.SharpeRatio)
		flog.WriteString(line)
		log.Warn(line)
		curScore := -loss
		odNumRate := float64(bt.OrderNum) / float64(bestOdNum)
		scoreRate := curScore / bestScore
		if scoreRate > 1.25 || scoreRate > 1.1 && odNumRate < 1.5 {
			bestScore = curScore
			bestOdNum = bt.OrderNum
			bestPols = []*config.RunPolicyConfig{long, short}
		}
	}
	if minScore < 0 || maxScore > minScore*5 {
		// The long and short returns are seriously unbalanced, the parameters with high fixed returns remain unchanged, and the parameters with low returns are fine-tuned to find the best score of the combination
		// 多空收益严重不均衡，固定收益高的参数不变，微调收益低的参数，寻找组合最佳分数
		config.RunPolicy = []*config.RunPolicyConfig{long, short}
		var unionScore float64
		if long.Score > short.Score {
			optForPol(short, method, picker, rounds, flog)
			unionScore = short.Score
		} else {
			optForPol(long, method, picker, rounds, flog)
			unionScore = long.Score
		}
		if unionScore > bestScore {
			return &GroupScore{[]*config.RunPolicyConfig{long, short}, unionScore}
		}
	}
	if len(bestPols) > 0 {
		return &GroupScore{bestPols, bestScore}
	}
	return nil
}

type GroupScore struct {
	Items []*config.RunPolicyConfig
	Score float64
}

/*
optForPol
Optimize policy tasks and support bayes, tpe, and cames
Before calling this method, you need to set 'config. RunPolicy`
对策略任务执行优化，支持bayes/tpe/cames等
调用此方法前需要设置 `config.RunPolicy`
*/
func optForPol(pol *config.RunPolicyConfig, method, picker string, rounds int, flog *os.File) {
	title := pol.Key()
	// 重置PairParams，避免影响传入参数
	pol.PairParams = make(map[string]map[string]float64)
	pol.Score = -998
	_ = strat.New(pol)
	params := pol.HyperParams()
	if len(params) == 0 {
		log.Warn("no hyper params, skip optimize", zap.String("strtg", title))
		return
	}
	flog.WriteString(fmt.Sprintf("\n============== %s =============\n", title))
	var resList = make([]*OptInfo, 0, rounds)
	runOptJob := func(data map[string]float64) (float64, *errs.Error) {
		for k, v := range data {
			pol.Params[k] = v
		}
		bt, loss := runBTOnce()
		line := fmt.Sprintf("%s \todNum: %v, profit: %.1f%%, drawDown: %.1f%%, sharpe: %.2f\n",
			paramsToStr(data, loss), bt.OrderNum, bt.TotProfitPct, bt.MaxDrawDownPct, bt.SharpeRatio)
		flog.WriteString(line)
		log.Warn(line)
		resList = append(resList, &OptInfo{
			Params:   data,
			Score:    -loss,
			BTResult: bt.BTResult,
		})
		return loss, nil
	}
	var err *errs.Error
	if method == "bayes" {
		err = runBayes(rounds, params, runOptJob)
	} else {
		err = runGOptuna(method, rounds, params, runOptJob)
	}
	best := calcBestBy(resList, picker)
	if best.BTResult == nil {
		best.runGetBtResult(pol)
	}
	if err != nil {
		log.Error("optimize fail", zap.String("job", title), zap.Error(err))
	} else {
		line := "[best] " + paramsToStr(best.Params, -best.Score)
		flog.WriteString(line + "\n")
		log.Warn(line)
	}
	pol.Params = best.Params
	pol.Score = best.Score
	pol.MaxOpen = best.OrderNum
}

func runBTOnce() (*BackTest, float64) {
	core.BotRunning = true
	biz.ResetVars()
	bt := NewBackTest(true)
	bt.Run()
	var loss float64
	if bt.TotProfitPct <= 0 {
		loss = -bt.TotProfitPct
	} else {
		// 盈利时返回无回撤收益率
		loss = -bt.TotProfitPct * math.Pow(1-bt.MaxDrawDownPct/100, 1.5)
	}
	return bt, loss
}

func runGOptuna(name string, rounds int, params []*core.Param, loop FuncOptTask) *errs.Error {
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
		return errs.New(errs.CodeRunTime, err_)
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
		return errs.New(errs.CodeRunTime, err_)
	}
	return nil
}

func runBayes(rounds int, params []*core.Param, loop FuncOptTask) *errs.Error {
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
		bayesopt.WithRandomRounds(rounds / 2),
	}
	opt := bayesopt.New(bysParams, options...)
	_, _, err_ := opt.Optimize(func(m map[bayesopt.Param]float64) float64 {
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
	if err_ != nil {
		return errs.New(errs.CodeRunTime, err_)
	}
	err_ = opt.ExplorationErr()
	if err_ != nil {
		log.Warn("bayes early stop", zap.String("err", err_.Error()))
	}
	return nil
}

func paramsToStr(m map[string]float64, loss float64) string {
	text, numLen := utils.MapToStr(m)
	tabLack := (len(m)*5 - numLen) / 4
	if tabLack > 0 {
		text += strings.Repeat("\t", tabLack)
	}
	return fmt.Sprintf("loss: %7.2f \t%s", loss, text)
}

/*
CollectOptLog
Collect and analyze the logs generated by RunOptimize
Sorts all policy tasks in reverse score order of output.
收集分析RunOptimize生成的日志
将所有策略任务按分数倒序排列输出。
*/
func CollectOptLog(args *config.CmdArgs) *errs.Error {
	if args.InPath == "" {
		log.Warn("-in is required")
		return nil
	}
	core.SetRunMode(core.RunModeBackTest)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	paths := make([]string, 0)
	filepath.WalkDir(args.InPath, func(path string, d fs.DirEntry, err error) error {
		if strings.HasSuffix(path, ".log") {
			paths = append(paths, path)
		}
		return nil
	})
	res, err := collectOptLog(paths, 0, args.Picker)
	if err != nil {
		return err
	}
	fmt.Print(res)
	return nil
}

func collectOptLog(paths []string, minScore float64, picker string) (string, *errs.Error) {
	res := make([]*OptGroup, 0)
	for _, path := range paths {
		var name, pair, dirt, tfStr string
		inUnion := false
		var items []*OptInfo
		var long, short, both, union, longMain, shortMain *OptInfo
		fdata, err_ := os.ReadFile(path)
		if err_ != nil {
			return "", errs.New(errs.CodeIOReadFail, err_)
		}
		saveGroup := func() {
			if name == "" {
				return
			}
			var oneSide = long
			if short != nil && (oneSide == nil || short.Score > oneSide.Score) {
				oneSide = short
			}
			var twoSide = both
			if union != nil && (twoSide == nil || union.Score > twoSide.Score) {
				twoSide = union
			}
			sideMain := 0
			if longMain != nil && (twoSide == nil || longMain.Score > twoSide.Score) {
				twoSide = longMain
				sideMain = 1
			}
			if shortMain != nil && (twoSide == nil || shortMain.Score > twoSide.Score) {
				twoSide = shortMain
				sideMain = 2
			}
			var pols []*OptInfo
			var bestScore float64
			var useOne = true
			if twoSide == nil {
				useOne = true
			} else if oneSide == nil {
				useOne = false
			} else {
				scoreRate := twoSide.Score / oneSide.Score
				odNumRate := float64(twoSide.OrderNum) / float64(oneSide.OrderNum)
				if scoreRate > 1.25 || scoreRate > 1.1 && odNumRate < 1.5 {
					useOne = false
				}
			}
			if !useOne {
				bestScore = twoSide.Score
				if twoSide.Dirt == "long" {
					long.Params = twoSide.Params
				} else if twoSide.Dirt == "short" {
					short.Params = twoSide.Params
				}
				if twoSide.Dirt == "" {
					pols = []*OptInfo{twoSide}
				} else if sideMain == 1 {
					pols = []*OptInfo{long, longMain}
				} else if sideMain == 2 {
					pols = []*OptInfo{short, shortMain}
				} else {
					pols = []*OptInfo{long, short}
				}
			} else {
				bestScore = oneSide.Score
				pols = []*OptInfo{oneSide}
			}
			res = append(res, &OptGroup{
				Items: pols,
				Score: bestScore,
				Name:  name,
				Pair:  pair,
				TFStr: tfStr,
			})
			long, short, both, union, longMain, shortMain = nil, nil, nil, nil, nil, nil
			name, pair, dirt, tfStr = "", "", "", ""
			inUnion = false
			items = nil
		}
		lines := strings.Split(string(fdata), "\n")[1:]
		for _, line := range lines {
			if line == "" || strings.HasPrefix(line, "[best]") {
				// end section, calc best
				best := calcBestBy(items, picker)
				if best != nil {
					needRun := best.BTResult == nil
					if inUnion {
						union = best
						union.Dirt = "union"
						inUnion = false
						if needRun {
							config.RunPolicy = []*config.RunPolicyConfig{
								long.ToPol(name, dirt, tfStr, pair),
								short.ToPol(name, dirt, tfStr, pair),
							}
						}
					} else if dirt == "long" {
						if long == nil {
							long = best
							if needRun {
								config.RunPolicy = []*config.RunPolicyConfig{
									long.ToPol(name, dirt, tfStr, pair),
								}
							}
						} else {
							shortMain = best
							if needRun {
								config.RunPolicy = []*config.RunPolicyConfig{
									short.ToPol(name, dirt, tfStr, pair),
									shortMain.ToPol(name, dirt, tfStr, pair),
								}
							}
						}
					} else if dirt == "short" {
						if short == nil {
							short = best
							if needRun {
								config.RunPolicy = []*config.RunPolicyConfig{
									short.ToPol(name, dirt, tfStr, pair)}
							}
						} else {
							longMain = best
							if needRun {
								config.RunPolicy = []*config.RunPolicyConfig{
									long.ToPol(name, dirt, tfStr, pair),
									longMain.ToPol(name, dirt, tfStr, pair)}
							}
						}
					} else {
						both = best
						if needRun {
							config.RunPolicy = []*config.RunPolicyConfig{both.ToPol(name, dirt, tfStr, pair)}
						}
					}
					if needRun {
						best.runGetBtResult(config.RunPolicy[len(config.RunPolicy)-1])
					}
				}
				items = nil
				continue
			}
			if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "# ") {
				// 跳过输出的配置、注释信息
				continue
			}
			if strings.HasPrefix(line, "==============") && strings.HasSuffix(line, "=============") {
				n, d, t, p := parseSectionTitle(strings.Split(line, " ")[1])
				if p != pair || n != name || t != tfStr {
					saveGroup()
				}
				name, dirt, tfStr, pair = n, d, t, p
				inUnion = false
			} else if strings.HasPrefix(line, "========== union") {
				inUnion = true
			} else if strings.HasPrefix(line, "loss:") {
				opt := parseOptLine(line)
				items = append(items, opt)
			}
		}
		saveGroup()
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].Score > res[j].Score
	})
	var b strings.Builder
	b.WriteString("run_policy:\n")
	for _, gp := range res {
		if gp.Score < minScore {
			break
		}
		b.WriteString(fmt.Sprintf("\n  # score: %.2f\n", gp.Score))
		for _, p := range gp.Items {
			tfStr := strings.ReplaceAll(gp.TFStr, "|", ", ")
			b.WriteString(fmt.Sprintf("  - name: %s\n    run_timeframes: [ %s ]\n", gp.Name, tfStr))
			if gp.Pair != "" {
				pairStr := strings.ReplaceAll(gp.Pair, "|", ", ")
				b.WriteString(fmt.Sprintf("    pairs: [%s]\n", pairStr))
			}
			if p.Dirt != "" {
				b.WriteString(fmt.Sprintf("    dirt: %s\n", p.Dirt))
			}
			paramStr, _ := utils.MapToStr(p.Params)
			b.WriteString(fmt.Sprintf("    params: {%s}\n", paramStr))
		}
	}
	return b.String(), nil
}

/*
Parse the header and return: policy name, direction, tfStr, pairStr
解析标题，返回：策略名，方向，tfStr，pairStr
*/
func parseSectionTitle(title string) (string, string, string, string) {
	arr := strings.Split(title, "/")
	name, dirt := parsePolID(arr[0])
	return name, dirt, arr[1], arr[2]
}

func parsePolID(id string) (string, string) {
	arr := strings.Split(id, ":")
	last := arr[len(arr)-1]
	var name, dirt string
	name = strings.Join(arr[:len(arr)-1], ":")
	if last == "l" {
		dirt = "long"
	} else if last == "s" {
		dirt = "short"
	} else {
		name = strings.Join(arr, ":")
	}
	return name, dirt
}

func parseOptLine(line string) *OptInfo {
	if !strings.HasPrefix(line, "loss:") {
		return nil
	}
	paraStart := strings.IndexRune(line, '\t')
	paraEnd := strings.Index(line, "odNum:")
	res := &OptInfo{Params: make(map[string]float64), BTResult: &BTResult{}}
	loss, _ := strconv.ParseFloat(strings.TrimSpace(line[5:paraStart]), 64)
	res.Score = -loss
	paraArr := strings.Split(strings.TrimSpace(line[paraStart:paraEnd]), ",")
	for _, str := range paraArr {
		arr := strings.Split(strings.TrimSpace(str), ":")
		res.Params[arr[0]], _ = strconv.ParseFloat(strings.TrimSpace(arr[1]), 64)
	}
	prefArr := strings.Split(strings.TrimSpace(line[paraEnd:]), ",")
	for _, str := range prefArr {
		arr := strings.Split(strings.TrimSpace(str), ":")
		key, val := arr[0], strings.TrimSpace(arr[1])
		if key == "odNum" {
			res.OrderNum, _ = strconv.Atoi(val)
		} else if key == "profit" {
			res.TotProfitPct, _ = strconv.ParseFloat(val[:len(val)-1], 64)
		} else if key == "drawDown" {
			res.MaxDrawDownPct, _ = strconv.ParseFloat(val[:len(val)-1], 64)
		} else if key == "sharpe" {
			res.SharpeRatio, _ = strconv.ParseFloat(val, 64)
		}
	}
	return res
}
