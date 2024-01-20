package strategy

import (
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"slices"
)

/*
LoadStagyJobs 加载策略和交易对

	返回对应关系：[(pair, timeframe, 预热数量, 策略列表), ...]
*/
func LoadStagyJobs(pairs []string, tfScores map[string][]goods.TfScore) {
	if len(pairs) == 0 || len(tfScores) == 0 {
		panic("`pairs` and `tfScores` are required for LoadStagyJobs")
	}
	exgName := config.Exchange.Name
	tfs := make(map[string]bool)
	for _, pol := range config.RunPolicy {
		stagy := Get(pol.Name)
		if stagy == nil {
			panic(fmt.Sprintf("strategy %s load fail", pol.Name))
		}
		stagyMaxNum := pol.MaxPair
		if stagyMaxNum == 0 {
			stagyMaxNum = 999
		}
		stagJobs := core.GetStagyJobs(pol.Name)
		holdNum := len(stagJobs)
		for _, pair := range pairs {
			if holdNum > stagyMaxNum {
				break
			}
			if _, ok := stagJobs[pair]; ok {
				// 跳过此策略已有的交易对
				continue
			}
			scores, ok := tfScores[pair]
			if !ok {
				scores = make([]goods.TfScore, 0)
			}
			tf := stagy.pickTimeFrame(exgName, pair, scores)
			if tf == "" {
				log.Warn("LoadStagyJobs skip pair", zap.String("pair", pair),
					zap.String("stagy", stagy.Name), zap.Int("scores", len(scores)))
				continue
			}
			holdNum += 1
			tfs[tf] = true
			jobKey := fmt.Sprintf("%s_%s", pair, tf)
			items, ok := PairTFStags[jobKey]
			if !ok {
				items = make([]*TradeStagy, 0)
			}
			PairTFStags[jobKey] = append(items, stagy)
			core.StgPairTfs = append(core.StgPairTfs, &core.StgPairTf{Stagy: pol.Name, Pair: pair, TimeFrame: tf})
		}
	}
	for tf, _ := range tfs {
		core.TFSecs = append(core.TFSecs, &core.TFSecTuple{TF: tf, Secs: utils.TFToSecs(tf)})
	}
	slices.SortFunc(core.TFSecs, func(a, b *core.TFSecTuple) int {
		return a.Secs - b.Secs
	})
}
