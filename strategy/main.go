package strategy

import (
	"fmt"
	"github.com/anyongjin/banbot/config"
	"github.com/anyongjin/banbot/core"
	"github.com/anyongjin/banbot/log"
	"github.com/anyongjin/banbot/products"
	"github.com/anyongjin/banbot/utils"
	"go.uber.org/zap"
)

/*
LoadStagyJobs 加载策略和交易对

	返回对应关系：[(pair, timeframe, 预热数量, 策略列表), ...]
*/
func LoadStagyJobs(pairs []string, tfScores map[string][]products.TfScore) {
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
				scores = make([]products.TfScore, 0)
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
			items, ok := core.PairTFStags[jobKey]
			if !ok {
				items = make([]*TradeStagy, 0)
			}
			core.PairTFStags[jobKey] = append(items, stagy)
			core.StgPairTfs = append(core.StgPairTfs, &core.StgPairTf{Stagy: pol.Name, Pair: pair, TimeFrame: tf})
		}
	}
	for tf, _ := range tfs {
		core.TFSecs[tf] = utils.TFToSecs(tf)
	}
}
