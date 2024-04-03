package goods

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/mitchellh/mapstructure"
	"go.uber.org/zap"
	"gonum.org/v1/gonum/floats"
	"math"
	"slices"
	"time"
)

var (
	pairProducer IProducer
	filters      = make([]IFilter, 0, 10)
	lastRefresh  = int64(0)
	needTickers  = false
)

func Setup() *errs.Error {
	if len(config.PairFilters) == 0 {
		return nil
	}
	filters = make([]IFilter, 0, 10)
	for _, cfg := range config.PairFilters {
		var output IFilter
		var base = BaseFilter{Name: cfg.Name}
		switch cfg.Name {
		case "AgeFilter":
			output = &AgeFilter{BaseFilter: base}
		case "VolumePairList":
			output = &VolumePairFilter{BaseFilter: base}
		case "PriceFilter":
			base.NeedTickers = true
			output = &PriceFilter{BaseFilter: base}
		case "RateOfChangeFilter":
			output = &RateOfChangeFilter{BaseFilter: base}
		case "VolatilityFilter":
			output = &VolatilityFilter{BaseFilter: base}
		case "SpreadFilter":
			base.NeedTickers = true
			output = &SpreadFilter{BaseFilter: base}
		case "OffsetFilter":
			output = &OffsetFilter{BaseFilter: base}
		case "ShuffleFilter":
			output = &ShuffleFilter{BaseFilter: base}
		default:
			return errs.NewMsg(errs.CodeParamInvalid, "unknown symbol filter: %s", cfg.Name)
		}
		err_ := mapstructure.Decode(cfg.Items, &output)
		if err_ != nil {
			return errs.New(errs.CodeUnmarshalFail, err_)
		}
		filters = append(filters, output)
	}
	for i, flt := range filters {
		if i == 0 {
			producer, ok := flt.(IProducer)
			if !ok {
				return errs.NewMsg(core.ErrBadConfig, "first pair filter must be IProducer")
			}
			pairProducer = producer
			continue
		}
		if flt.IsNeedTickers() {
			needTickers = true
			break
		}
	}
	filters = filters[1:]
	return nil
}

func RefreshPairList(addPairs []string) (map[string]map[string]float64, *errs.Error) {
	lastRefresh = btime.TimeMS()
	var pairs []string
	var allowFilter = false
	var err *errs.Error
	var tickersMap map[string]*banexg.Ticker
	if len(config.Pairs) > 0 {
		pairs = config.Pairs
	} else {
		allowFilter = true
		exchange := exg.Default
		if needTickers && core.LiveMode {
			tickersMap = core.GetCacheVal("tickers", map[string]*banexg.Ticker{})
			if len(tickersMap) == 0 {
				tickers, err := exchange.FetchTickers(nil, nil)
				if err != nil {
					return nil, err
				}
				for _, t := range tickers {
					tickersMap[t.Symbol] = t
				}
				expires := time.Second * 3600
				core.Cache.SetWithTTL("tickers", tickersMap, 1, expires)
			}
		}
		genPairs, err := pairProducer.GenSymbols(tickersMap)
		if err != nil {
			return nil, err
		}
		for _, pair := range genPairs {
			_, quote, _, _ := core.SplitSymbol(pair)
			if _, ok := config.StakeCurrencyMap[quote]; ok {
				pairs = append(pairs, pair)
			}
		}
		log.Info(fmt.Sprintf("gen symbols from %s, num: %d", pairProducer.GetName(), len(pairs)))
	}
	err = orm.EnsureCurSymbols(pairs)
	if err != nil {
		return nil, err
	}
	if allowFilter {
		for _, flt := range filters {
			if flt.IsDisable() {
				continue
			}
			oldNum := len(pairs)
			pairs, err = flt.Filter(pairs, tickersMap)
			if err != nil {
				return nil, err
			}
			if oldNum > len(pairs) {
				log.Info(fmt.Sprintf("left %d symbols after %s", len(pairs), flt.GetName()))
			}
		}
	}
	adds := utils.UnionArr(addPairs, config.GetExgConfig().WhitePairs)
	if len(adds) > 0 {
		err = orm.EnsureCurSymbols(adds)
		if err != nil {
			return nil, err
		}
		pairs = utils.UnionArr(adds, pairs)
	}
	// 数量和偏移限制
	mgrCfg := config.PairMgr
	if mgrCfg.Offset < len(pairs) {
		pairs = pairs[mgrCfg.Offset:]
	} else if mgrCfg.Offset > 0 {
		pairs = nil
	}
	if mgrCfg.Limit < len(pairs) {
		pairs = pairs[mgrCfg.Limit:]
	}

	core.Pairs = nil
	core.PairsMap = make(map[string]bool)
	for _, p := range pairs {
		core.Pairs = append(core.Pairs, p)
		core.PairsMap[p] = true
	}
	slices.Sort(core.Pairs)
	// 计算交易对各维度K线质量分数
	return calcPairTfScales(exg.Default, pairs)
}

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
	handle := func(pair, timeFrame string, arr []*banexg.Kline) {
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
		stagy := strategy.Get(pol.Name)
		if stagy == nil {
			continue
		}
		if len(pol.RunTimeframes) > 0 {
			groups = append(groups, pol.RunTimeframes)
			// 配置的时间周期优先级高于策略写死的
			stagy.AllowTFs = pol.RunTimeframes
		} else {
			groups = append(groups, stagy.AllowTFs)
		}
	}
	return utils.UnionArr(groups...)
}
