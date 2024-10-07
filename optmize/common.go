package optmize

import (
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/mitchellh/mapstructure"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
)

type FnCalcOptBest = func(items []*OptInfo) *OptInfo

var (
	MapCalcOptBest = map[string]FnCalcOptBest{
		"score": getBestByScore,
	}
)

type OptGroup struct {
	Items []*OptInfo
	Score float64
	Name  string
	Pair  string
	TFStr string
}

type OptInfo struct {
	Dirt   string
	Score  float64
	Params map[string]float64
	*BTResult
}

type rollBtOpt struct {
	args        *config.CmdArgs
	curMs       int64
	allEndMs    int64
	dateRange   *config.TimeTuple
	runMSecs    int64
	reviewMSecs int64
	outDir      string
	initPols    []*config.RunPolicyConfig
}

type ValItem struct {
	Tag   string
	Score float64
	Order int
	Res   int
}

func getBestByScore(items []*OptInfo) *OptInfo {
	var best *OptInfo
	for _, it := range items {
		if best == nil || it.Score > best.Score {
			best = it
		}
	}
	return best
}

func calcBestBy(items []*OptInfo, name string) *OptInfo {
	if len(items) == 0 {
		return nil
	}
	method, _ := MapCalcOptBest[name]
	if method == nil {
		if name != "" {
			log.Warn("picker for MapCalcOptBest not found, use default", zap.String("n", name))
		}
		method = getBestByScore
	}
	res := method(items)
	if res == nil {
		res = getBestByScore(items)
	}
	return res
}

func (o *OptInfo) runGetBtResult(pol *config.RunPolicyConfig) {
	if o.BTResult == nil {
		for k, v := range o.Params {
			pol.Params[k] = v
		}
		bt, loss := runBTOnce()
		o.Score = -loss
		o.BTResult = bt.BTResult
	}
}

func (o *OptInfo) ToPol(name, dirt, tfStr, pairStr string) *config.RunPolicyConfig {
	if o.Dirt == "" {
		o.Dirt = dirt
	}
	res := &config.RunPolicyConfig{
		Name:   name,
		Dirt:   o.Dirt,
		Params: o.Params,
		Score:  o.Score,
	}
	if len(tfStr) > 0 {
		res.RunTimeframes = strings.Split(tfStr, "|")
	}
	if len(pairStr) > 0 {
		res.Pairs = strings.Split(pairStr, "|")
	}
	return res
}

func newRollBtOpt(args *config.CmdArgs) (*rollBtOpt, *errs.Error) {
	core.SetRunMode(core.RunModeBackTest)
	err := biz.SetupComs(args)
	if err != nil {
		return nil, err
	}
	err = orm.InitExg(exg.Default)
	if err != nil {
		return nil, err
	}
	dateRange := config.TimeRange
	allStartMs, allEndMs := dateRange.StartMS, dateRange.EndMS
	runMSecs := int64(utils.TFToSecs(args.RunPeriod)) * 1000
	reviewMSecs := int64(utils.TFToSecs(args.ReviewPeriod)) * 1000
	if runMSecs < core.SecsHour*1000 {
		return nil, errs.NewMsg(errs.CodeParamInvalid, "`run-period` cannot be less than 1 hour")
	}
	outDir := filepath.Join(config.GetDataDir(), "backtest", "bt_opt_"+btOptHash(args))
	err_ := utils.EnsureDir(outDir, 0755)
	if err_ != nil {
		return nil, errs.New(errs.CodeIOWriteFail, err_)
	}
	log.Info("write bt over opt to", zap.String("dir", outDir))
	args.OutPath = filepath.Join(outDir, "opt.log")
	curMs := allStartMs + reviewMSecs
	initPols := config.RunPolicy
	return &rollBtOpt{
		args:        args,
		curMs:       curMs,
		allEndMs:    allEndMs,
		dateRange:   dateRange,
		runMSecs:    runMSecs,
		reviewMSecs: reviewMSecs,
		outDir:      outDir,
		initPols:    initPols,
	}, nil
}

func (t *rollBtOpt) next() (string, *errs.Error) {
	t.dateRange.StartMS = t.curMs - t.reviewMSecs
	t.dateRange.EndMS = t.curMs
	fname := fmt.Sprintf("opt_%v.log", t.dateRange.StartMS/1000)
	t.args.OutPath = filepath.Join(t.outDir, fname)
	polStr, err := pickFromExists(t.args.OutPath, t.args.Picker)
	if err != nil {
		return "", err
	}
	if polStr == "" {
		config.RunPolicy = t.initPols
		polStr, err = runOptimize(t.args, 11)
		if err != nil {
			return "", err
		}
	}
	return polStr, nil
}

func (t *rollBtOpt) dumpConfig() *errs.Error {
	data, err := config.DumpYaml()
	if err != nil {
		return err
	}
	outPath := filepath.Join(t.outDir, "config.yml")
	err_ := os.WriteFile(outPath, data, 0644)
	if err_ != nil {
		return errs.New(errs.CodeIOWriteFail, err_)
	}
	return nil
}

func parseRunPolicies(text string) ([]*config.RunPolicyConfig, *errs.Error) {
	var unpak = make(map[string]interface{})
	err_ := yaml.Unmarshal([]byte(text), &unpak)
	if err_ != nil {
		return nil, errs.New(errs.CodeRunTime, err_)
	}
	var cfg config.Config
	err_ = mapstructure.Decode(unpak, &cfg)
	if err_ != nil {
		return nil, errs.New(errs.CodeRunTime, err_)
	}
	return cfg.RunPolicy, nil
}

func (r *BTResult) BriefLine() string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf("odNum: %v, profit: %.1f%%, drawDown: %.1f%%, sharpe: %.2f",
		r.OrderNum, r.TotProfitPct, r.MaxDrawDownPct, r.SharpeRatio)
}

func (o *OptInfo) ToLine() string {
	var text string
	if o.Params != nil && len(o.Params) > 0 {
		var numLen int
		text, numLen = utils.MapToStr(o.Params)
		tabLack := (len(o.Params)*5 - numLen) / 4
		if tabLack > 0 {
			text += strings.Repeat("\t", tabLack)
		}
	}
	return fmt.Sprintf("loss: %7.2f \t%s \t%s", -o.Score, text, o.BriefLine())
}
