package optmize

import (
	"github.com/banbox/banbot/config"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
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
