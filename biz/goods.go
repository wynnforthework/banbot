package biz

import (
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"gonum.org/v1/gonum/floats"
	"math"
	"slices"
)

// 计算交易对各维度K线质量分数
func calcPairTfScales(exchange banexg.BanExchange, pairs []string) (map[string]map[string]float64, *errs.Error) {
	pairTfScores := make(map[string]map[string]float64)
	allowTfs := allAllowTFs()
	if len(allowTfs) == 0 {
		return pairTfScores, errs.NewMsg(core.ErrBadConfig, "run_timeframes is required in config")
	}
	wsModeTf := ""
	for _, v := range allowTfs {
		tfSecs := utils.TFToSecs(v)
		if tfSecs < 60 {
			wsModeTf = v
			break
		}
	}
	if wsModeTf != "" {
		for _, pair := range pairs {
			pairTfScores[pair] = map[string]float64{wsModeTf: 1.0}
		}
		return pairTfScores, nil
	}
	handle := func(pair, timeFrame string, arr []*banexg.Kline, adjs []*orm.AdjInfo) {
		tfScores, ok := pairTfScores[pair]
		if !ok {
			tfScores = make(map[string]float64)
			pairTfScores[pair] = tfScores
		}
		pipChg, err := exchange.PriceOnePip(pair)
		if err != nil {
			log.Error("PriceOnePip fail", zap.String("pair", pair), zap.Float64("pip", pipChg))
			return
		}
		score := float64(1)
		if len(arr) > 0 && pipChg > 0 {
			arr = orm.ApplyAdj(adjs, arr, core.AdjFront, 0, 0)
			score = calcKlineScore(arr, pipChg, 3)
		}
		tfScores[timeFrame] = score
	}
	backNum := 600
	for _, tf := range allowTfs {
		err := orm.FastBulkOHLCV(exchange, pairs, tf, 0, 0, backNum, handle)
		if err != nil {
			return pairTfScores, err
		}
	}
	return pairTfScores, nil
}

/*
calcKlineScore
计算K线质量。用于淘汰变动太小，波动不足的交易对；或计算交易对的最佳周期。阈值取0.8较合适

	价格变动：四价相同-1分；bar变动=最小变动单位-1分 40%权重
	平均加权实体占比：30%权重
	Bar重合比率分数，某个bar的实体部分，与前两个bar的实体范围重合区间，占当前bar实体的比率，越低分数越高，30%权重
*/
func calcKlineScore(arr []*banexg.Kline, pipChg float64, prevNum int) float64 {
	totalLen := len(arr)
	pipScore := float64(len(arr))
	solidRates := make([]float64, 0, len(arr))
	overlaps := make([]float64, 0, len(arr))
	olWeightSum, solWeightSum := float64(0), float64(0)
	var pRanges = make([]float64, prevNum*2)
	var nextIdx int
	var cMin, cMax float64
	for i, bar := range arr {
		chgRate := (bar.High - bar.Low) / pipChg
		if chgRate == 0 || chgRate == 1 {
			pipScore -= 1
		} else if chgRate == 2 {
			pipScore -= 0.3
		}
		cMin, cMax = bar.Low, bar.High
		barLen := cMax - cMin
		weight := barLen / cMax
		if cMin != cMax {
			rate := math.Abs(bar.Open-bar.Close) / barLen
			solidRates = append(solidRates, rate*weight)
			solWeightSum += weight
		}
		if i >= prevNum && cMin != cMax {
			pMax, pMin := slices.Max(pRanges), slices.Min(pRanges)
			// 计算当前bar与前面n个bar的重合率
			olRate := max(min(cMax, pMax)-max(cMin, pMin), 0) / barLen
			overlaps = append(overlaps, olRate*weight)
			olWeightSum += weight
		}
		pRanges[nextIdx] = cMin
		pRanges[nextIdx+1] = cMax
		nextIdx = (nextIdx + 2) % (prevNum * 2)
	}
	// 价格变动单位分数
	chgScore := math.Pow(pipScore/float64(totalLen), 2)
	// 实体部分占比分数
	jRateScore := floats.Sum(solidRates) / solWeightSum
	jRateScore = 1 - math.Pow(1-jRateScore, 2)
	// 计算与前面n个bar重合率分数
	overlapScore := 1 - floats.Sum(overlaps)/olWeightSum
	overlapScore = 1 - math.Pow(1-overlapScore, 3)
	return chgScore*0.4 + jRateScore*0.3 + overlapScore*0.3
}

func allAllowTFs() []string {
	var groups = [][]string{config.RunTimeframes}
	for _, pol := range config.RunPolicy {
		if pol.Dirt == "any" {
			pol.Dirt = ""
		}
		stagy := strategy.New(pol)
		if stagy == nil {
			continue
		}
		if len(pol.RunTimeframes) > 0 {
			// 配置的时间周期优先级高于策略写死的
			groups = append(groups, pol.RunTimeframes)
		} else {
			groups = append(groups, stagy.AllowTFs)
		}
	}
	return utils.UnionArr(groups...)
}
