package optmize

import (
	"bytes"
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	ta "github.com/banbox/banta"
	"github.com/c-bata/goptuna"
	"github.com/c-bata/goptuna/cmaes"
	"github.com/c-bata/goptuna/tpe"
	"github.com/d4l3k/go-bayesopt"
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
RunBTOverOpt 基于持续调参的回测模式。接近实盘情况，避免使用未来信息调参回测。
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
	outDir := filepath.Join(config.GetDataDir(), "backtest", "bt_opt")
	err_ := utils.EnsureDir(outDir, 0755)
	if err_ != nil {
		return errs.New(errs.CodeIOWriteFail, err_)
	}
	args.OutPath = filepath.Join(outDir, "opt.log")
	curMs := allStartMs + reviewMSecs
	var allHisOds []*orm.InOutOrder
	var lastWal map[string]float64
	var lastRes *BTResult
	for curMs < allEndMs {
		dateRange.StartMS = curMs - reviewMSecs
		dateRange.EndMS = curMs
		fname := fmt.Sprintf("pol_%v_%v.yml", dateRange.StartMS/1000, curMs/1000)
		cfgPath := filepath.Join(outDir, fname)
		var polData []byte
		if !utils.Exists(cfgPath) {
			polStr, err := runOptimize(args, 11)
			if err != nil {
				return err
			}
			polData = []byte(polStr)
			err_ = utils2.WriteFile(cfgPath, polData)
			if err_ != nil {
				log.Warn("write pol cache fail", zap.Error(err_))
			}
		} else {
			polData, err_ = utils2.ReadFile(cfgPath)
			if err_ != nil {
				return errs.New(errs.CodeIOReadFail, err_)
			}
		}
		ResetVars()
		var unpak = make(map[string]interface{})
		err_ = yaml.Unmarshal(polData, &unpak)
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
		config.RunPolicy = cfg.RunPolicy
		wallets := biz.GetWallets("")
		core.BotRunning = true
		dateRange.StartMS = curMs
		dateRange.EndMS = curMs + runMSecs
		bt := NewBackTest()
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

func RunOptimize(args *config.CmdArgs) *errs.Error {
	if args.OutPath == "" {
		log.Warn("-out is required")
		return nil
	}
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
			// 针对每个策略、多空单独进行贝叶斯优化，寻找最佳参数
			err = optAndPrint(gp.Clone(), args, allPairs, file)
			if err != nil {
				return "", err
			}
		}
	} else {
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
		logPath := strings.TrimSuffix(args.OutPath, filepath.Ext(args.OutPath))
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
			outPath := logPath + iStr + ".log"
			curCmds = append(curCmds, "-out", outPath)
			logOuts = append(logOuts, outPath)
			log.Warn("runing: " + strings.Join(curCmds, " "))
			var out bytes.Buffer
			prgName := "banbot.o"
			if runtime.GOOS == "windows" {
				prgName = "banbot.exe"
			}
			excPath := filepath.Join(config.GetStagyDir(), prgName)
			if _, err_ = os.Stat(excPath); err_ != nil {
				return errs.New(errs.CodeRunTime, err_)
			}
			cmd := exec.Command(excPath, curCmds...)
			cmd.Dir = config.GetStagyDir()
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
	return collectOptLog(logOuts, minScore)
}

func optAndPrint(pol *config.RunPolicyConfig, args *config.CmdArgs, allPairs []string, file *os.File) *errs.Error {
	file.WriteString(fmt.Sprintf("# run hyper optimize: %v\n", args.Sampler))
	startDt := btime.ToDateStr(config.TimeRange.StartMS, "")
	endDt := btime.ToDateStr(config.TimeRange.EndMS, "")
	file.WriteString(fmt.Sprintf("# date range: %v - %v\n", startDt, endDt))
	res := make([]*GroupScore, 0, 5)
	if args.EachPairs {
		pairs := pol.Pairs
		if len(pairs) == 0 {
			pairs = allPairs
		}
		for _, p := range pairs {
			pol.Pairs = []string{p}
			item := optForGroup(pol, args.Sampler, args.OptRounds, file)
			if item != nil {
				res = append(res, item)
			}
		}
	} else {
		item := optForGroup(pol, args.Sampler, args.OptRounds, file)
		if item != nil {
			res = append(res, item)
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].Score > res[j].Score
	})
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
optForGroup 对某个策略超参数调优，自动搜索long/short/both的最佳组合。
*/
func optForGroup(pol *config.RunPolicyConfig, method string, rounds int, flog *os.File) *GroupScore {
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
		optForPol(p, method, rounds, flog)
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
		// 多空收益严重不均衡，固定收益高的参数不变，微调收益低的参数，寻找组合最佳分数
		config.RunPolicy = []*config.RunPolicyConfig{long, short}
		var unionScore float64
		if long.Score > short.Score {
			optForPol(short, method, rounds, flog)
			unionScore = short.Score
		} else {
			optForPol(long, method, rounds, flog)
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
optForPol 对策略任务执行优化，支持bayes/tpe/cames等
调用此方法前需要设置 `config.RunPolicy`
*/
func optForPol(pol *config.RunPolicyConfig, method string, rounds int, flog *os.File) {
	tfStr := strings.Join(pol.RunTimeframes, "|")
	pairStr := strings.Join(pol.Pairs, "|")
	title := fmt.Sprintf("%s/%s/%s", pol.ID(), tfStr, pairStr)
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
	var minLoss = float64(998)
	var best map[string]float64
	var bestBt *BTResult
	runOptJob := func(data map[string]float64) (float64, *errs.Error) {
		for k, v := range data {
			pol.Params[k] = v
		}
		bt, loss := runBTOnce()
		line := fmt.Sprintf("%s \todNum: %v, profit: %.1f%%, drawDown: %.1f%%, sharpe: %.2f\n",
			paramsToStr(data, loss), bt.OrderNum, bt.TotProfitPct, bt.MaxDrawDownPct, bt.SharpeRatio)
		flog.WriteString(line)
		log.Warn(line)
		if loss < minLoss {
			minLoss = loss
			best = data
			bestBt = bt.BTResult
		}
		return loss, nil
	}
	var err *errs.Error
	if method == "bayes" {
		err = runBayes(rounds, params, runOptJob)
	} else {
		err = runGOptuna(method, rounds, params, runOptJob)
	}
	if err != nil {
		log.Error("optimize fail", zap.String("job", title), zap.Error(err))
	} else {
		line := "[best] " + paramsToStr(best, minLoss)
		flog.WriteString(line + "\n")
		log.Warn(line)
	}
	pol.Params = best
	pol.Score = -minLoss
	pol.MaxOpen = bestBt.OrderNum
}

func runBTOnce() (*BackTest, float64) {
	core.BotRunning = true
	ResetVars()
	bt := NewBackTest()
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
		bayesopt.WithRandomRounds(rounds / 3),
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

func ResetVars() {
	core.NoEnterUntil = make(map[string]int64)
	core.PairCopiedMs = make(map[string][2]int64)
	core.TfPairHits = make(map[string]map[string]int)
	biz.ResetVars()
	core.LastBarMs = 0
	core.OdBooks = make(map[string]*banexg.OrderBook)
	orm.HistODs = make([]*orm.InOutOrder, 0)
	//orm.FakeOdId = 1
	orm.ResetVars()
	strat.Envs = make(map[string]*ta.BarEnv)
	strat.AccJobs = make(map[string]map[string]map[string]*strat.StagyJob)
	strat.AccInfoJobs = make(map[string]map[string]map[string]*strat.StagyJob)
	strat.PairStags = make(map[string]map[string]*strat.TradeStagy)
	strat.BatchTasks = make(map[string]*strat.BatchMap)
	strat.LastBatchMS = 0
}

/*
CollectOptLog 收集分析RunOptimize生成的日志
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
	res, err := collectOptLog(paths, 0)
	if err != nil {
		return err
	}
	fmt.Print(res)
	return nil
}

func collectOptLog(paths []string, minScore float64) (string, *errs.Error) {
	res := make([]*OptGroup, 0)
	for _, path := range paths {
		var name, pair, dirt, tfStr string
		var best *OptInfo
		inUnion := false
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
			if longMain != nil && (twoSide == nil || longMain.Score > twoSide.Score) {
				twoSide = longMain
			}
			if shortMain != nil && (twoSide == nil || shortMain.Score > twoSide.Score) {
				twoSide = shortMain
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
				odNumRate := float64(twoSide.OdNum) / float64(oneSide.OdNum)
				if scoreRate > 1.25 || scoreRate > 1.1 && odNumRate < 1.5 {
					useOne = false
				}
			}
			if !useOne {
				bestScore = twoSide.Score
				if twoSide.Dirt == "long" {
					long.Param = twoSide.Param
				} else if twoSide.Dirt == "short" {
					short.Param = twoSide.Param
				}
				if twoSide.Dirt == "" {
					pols = []*OptInfo{twoSide}
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
			best, inUnion = nil, false
		}
		lines := strings.Split(string(fdata), "\n")[1:]
		for _, line := range lines {
			if line == "" || strings.HasPrefix(line, "[best]") {
				if best != nil {
					if inUnion {
						union = best
						union.Dirt = "union"
						inUnion = false
					} else if dirt == "long" {
						if long == nil {
							long = best
						} else {
							shortMain = best
						}
					} else if dirt == "short" {
						if short == nil {
							short = best
						} else {
							longMain = best
						}
					} else {
						both = best
					}
					best = nil
				}
				continue
			}
			if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "# ") {
				// 跳过输出的配置、注释信息
				continue
			}
			if strings.HasPrefix(line, "==============") && strings.HasSuffix(line, "=============") {
				n, d, t, p := parseSectionTitle(strings.Split(line, " ")[1])
				if p != pair {
					saveGroup()
				}
				name, dirt, tfStr, pair = n, d, t, p
				inUnion = false
			} else if strings.HasPrefix(line, "========== union") {
				inUnion = true
			} else if strings.HasPrefix(line, "loss:") {
				opt := parseOptLine(line)
				if best == nil || opt.Score > best.Score {
					best = opt
					best.Dirt = dirt
				}
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
			b.WriteString(fmt.Sprintf("    params: {%s}\n", p.Param))
		}
	}
	return b.String(), nil
}

/*
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

type OptGroup struct {
	Items []*OptInfo
	Score float64
	Name  string
	Pair  string
	TFStr string
}

type OptInfo struct {
	Dirt  string
	Score float64
	Param string
	OdNum int
}

func parseOptLine(line string) *OptInfo {
	raw := strings.Split(line, " ")
	row := make([]string, 0, len(raw))
	for _, text := range raw {
		if text == "" {
			continue
		}
		row = append(row, strings.TrimSpace(text))
	}
	num := len(row)
	cid := 0
	res := &OptInfo{}
	for cid+1 < num {
		tag, str := row[cid], row[cid+1]
		if tag == "loss:" {
			loss, _ := strconv.ParseFloat(str, 64)
			res.Score = -loss
		} else if tag == "odNum:" {
			odNum, _ := strconv.ParseInt(strings.ReplaceAll(str, ",", ""), 10, 64)
			res.OdNum = int(odNum)
			res.Param = strings.Join(row[2:cid], " ")
			break
		}
		cid += 2
	}
	return res
}
