package strategy

import (
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	ta "github.com/banbox/banta"
	"github.com/bytedance/sonic"
	"go.uber.org/zap"
	"slices"
	"strings"
)

/*
LoadStagyJobs 加载策略和交易对

	返回对应关系：[(pair, timeframe, 预热数量, 策略列表), ...]
*/
func LoadStagyJobs(pairs []string, tfScores map[string][]*core.TfScore) (map[string]map[string]int, *errs.Error) {
	if len(pairs) == 0 || len(tfScores) == 0 {
		return nil, errs.NewMsg(errs.CodeParamRequired, "`pairs` and `tfScores` are required for LoadStagyJobs")
	}
	var exsList []*orm.ExSymbol
	for _, pair := range pairs {
		exs, err := orm.GetExSymbolCur(pair)
		if err != nil {
			return nil, err
		}
		exsList = append(exsList, exs)
	}
	exgName := config.Exchange.Name
	tfs := make(map[string]bool)
	pairTfWarms := make(map[string]map[string]int)
	logWarm := func(pair, tf string, num int) {
		if warms, ok := pairTfWarms[pair]; ok {
			if oldNum, ok := warms[tf]; ok {
				warms[tf] = max(oldNum, num)
			} else {
				warms[tf] = num
			}
		} else {
			pairTfWarms[pair] = map[string]int{tf: num}
		}
	}
	for _, pol := range config.RunPolicy {
		stagy := Get(pol.Name)
		if stagy == nil {
			return pairTfWarms, errs.NewMsg(core.ErrRunTime, "strategy %s load fail", pol.Name)
		}
		Versions[stagy.Name] = stagy.Version
		stagyMaxNum := pol.MaxPair
		if stagyMaxNum == 0 {
			stagyMaxNum = 999
		}
		stagJobs := core.GetStagyJobs(pol.Name)
		holdNum := len(stagJobs)
		for _, exs := range exsList {
			if holdNum > stagyMaxNum {
				break
			}
			if _, ok := stagJobs[exs.Symbol]; ok {
				// 跳过此策略已有的交易对
				continue
			}
			scores, ok := tfScores[exs.Symbol]
			if !ok {
				scores = make([]*core.TfScore, 0)
			}
			tf := stagy.pickTimeFrame(exgName, exs.Symbol, scores)
			if tf == "" {
				scoreText, _ := sonic.MarshalString(scores)
				log.Warn("filter pair by tfScore", zap.String("pair", exs.Symbol),
					zap.String("stagy", stagy.Name), zap.String("scores", scoreText))
				continue
			}
			holdNum += 1
			tfs[tf] = true
			core.StgPairTfs = append(core.StgPairTfs, &core.StgPairTf{Stagy: pol.Name, Pair: exs.Symbol, TimeFrame: tf})
			if stagy.WatchBook {
				core.BookPairs[exs.Symbol] = true
			}
			// 初始化BarEnv
			envKey := strings.Join([]string{exs.Symbol, tf}, "_")
			tfMSecs := int64(utils.TFToSecs(tf) * 1000)
			env := &ta.BarEnv{
				Exchange:   core.ExgName,
				MarketType: core.Market,
				Symbol:     exs.Symbol,
				TimeFrame:  tf,
				TFMSecs:    tfMSecs,
				MaxCache:   core.NumTaCache,
				Data:       map[string]interface{}{"sid": exs.ID},
			}
			Envs[envKey] = env
			// 初始化交易任务
			job := &StagyJob{
				Stagy:     stagy,
				Env:       env,
				Symbol:    exs,
				TimeFrame: tf,
				TPMaxs:    make(map[int64]float64),
			}
			if jobs, ok := Jobs[envKey]; ok {
				Jobs[envKey] = append(jobs, job)
			} else {
				Jobs[envKey] = []*StagyJob{job}
			}
			// 记录需要预热的数据；记录订阅信息
			logWarm(exs.Symbol, tf, stagy.WarmupNum)
			if stagy.OnPairInfos != nil {
				for _, s := range stagy.OnPairInfos(job) {
					logWarm(s.Pair, s.TimeFrame, s.WarmupNum)
					jobKey := strings.Join([]string{s.Pair, s.TimeFrame}, "_")
					items, ok := InfoJobs[jobKey]
					if !ok {
						items = make([]*StagyJob, 0)
					}
					InfoJobs[jobKey] = append(items, job)
				}
			}
			items, ok := PairTFStags[envKey]
			if !ok {
				items = make([]*TradeStagy, 0)
			}
			PairTFStags[envKey] = append(items, stagy)
		}
	}
	core.TFSecs = nil
	for tf, _ := range tfs {
		core.TFSecs = append(core.TFSecs, &core.TFSecTuple{TF: tf, Secs: utils.TFToSecs(tf)})
	}
	slices.SortFunc(core.TFSecs, func(a, b *core.TFSecTuple) int {
		return a.Secs - b.Secs
	})
	return pairTfWarms, nil
}
