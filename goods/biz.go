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
	"github.com/dgraph-io/ristretto"
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
	cache        *ristretto.Cache
)

func init() {
	var err_ error
	cache, err_ = ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,
		MaxCost:     1 << 26,
		BufferItems: 64,
	})
	if err_ != nil {
		log.Error("init cache fail", zap.Error(err_))
	}
}

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

func RefreshPairList(addPairs []string) *errs.Error {
	lastRefresh = btime.TimeMS()
	var pairs []string
	var allowFilter = false
	var err *errs.Error
	var tickersMap map[string]*banexg.Ticker
	if !core.LiveMode() && len(config.Pairs) > 0 {
		pairs = config.Pairs
	} else {
		allowFilter = true
		exchange, err := exg.Get()
		if err != nil {
			return err
		}
		if needTickers && core.LiveMode() {
			tikCache, ok := cache.Get("tickers")
			if !ok {
				tickers, err := exchange.FetchTickers(nil, nil)
				if err != nil {
					return err
				}
				for _, t := range tickers {
					tickersMap[t.Symbol] = t
				}
				expires := time.Second * 3600
				cache.SetWithTTL("tickers", tickersMap, 1, expires)
			} else {
				tickersMap, _ = tikCache.(map[string]*banexg.Ticker)
			}
		}
		pairs, err = pairProducer.GenSymbols(tickersMap)
		if err != nil {
			return err
		}
		log.Info(fmt.Sprintf("gen symbols from %s, num: %d", pairProducer.GetName(), len(pairs)))
	}
	err = orm.EnsureCurSymbols(pairs)
	if err != nil {
		return err
	}
	if allowFilter {
		for _, flt := range filters {
			if flt.IsDisable() {
				continue
			}
			oldNum := len(pairs)
			pairs, err = flt.Filter(pairs, tickersMap)
			if err != nil {
				return err
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
			return err
		}
		pairs = utils.UnionArr(pairs, adds)
	}

	core.Pairs = nil
	core.PairsMap = make(map[string]bool)
	for _, p := range pairs {
		core.Pairs = append(core.Pairs, p)
		core.PairsMap[p] = true
	}
	slices.Sort(core.Pairs)
	// 计算交易对各维度K线质量分数
	exchange, err := exg.Get()
	if err != nil {
		return err
	}
	return calcPairTfScales(exchange, pairs)
}

func calcPairTfScales(exchange banexg.BanExchange, pairs []string) *errs.Error {
	allowTfs := allAllowTFs()
	if len(allowTfs) == 0 {
		return errs.NewMsg(core.ErrBadConfig, "run_timeframes is required in config")
	}
	wsModeTf := ""
	for _, v := range allowTfs {
		tfSecs := utils.TFToSecs(v)
		if tfSecs <= 60 {
			wsModeTf = v
			break
		}
	}
	if wsModeTf != "" {
		core.PairTfScores = make(map[string][]*core.TfScore)
		for _, pair := range pairs {
			core.PairTfScores[pair] = []*core.TfScore{{wsModeTf, 1.0}}
		}
		return nil
	}
	handle := func(pair, timeFrame string, arr []*banexg.Kline) {
		items, ok := core.PairTfScores[pair]
		if !ok {
			items = make([]*core.TfScore, 0, len(allowTfs))
		}
		pipChg, err := exchange.PriceOnePip(pair)
		if err != nil {
			log.Error("PriceOnePip fail", zap.String("pair", pair), zap.Float64("pip", pipChg))
			return
		}
		score := float64(1)
		if len(arr) > 0 && pipChg > 0 {
			score = calcKlineScore(arr, pipChg)
		}
		items = append(items, &core.TfScore{TF: timeFrame, Score: score})
		core.PairTfScores[pair] = items
	}
	backNum := 300
	for _, tf := range allowTfs {
		err := orm.FastBulkOHLCV(exchange, pairs, tf, 0, 0, backNum, handle)
		if err != nil {
			return err
		}
	}
	return nil
}

/*
calcKlineScore
计算K线质量。用于淘汰变动太小，波动不足的交易对；或计算交易对的最佳周期。阈值取0.8较合适

	价格变动：四价相同-1分；bar变动=最小变动单位-1分；70%权重
	平均跳空占比：30%权重

	改进点：目前无法量化横盘频繁密集震动。
*/
func calcKlineScore(arr []*banexg.Kline, pipChg float64) float64 {
	totalLen := len(arr)
	finScore := float64(len(arr))
	jumpRates := make([]float64, 0)
	var pBar *banexg.Kline
	for _, bar := range arr {
		chgRate := (bar.High - bar.Low) / pipChg
		if chgRate == 0 || chgRate == 1 {
			finScore -= 1
		} else if chgRate == 2 {
			finScore -= 0.3
		}
		if pBar != nil {
			nerMaxChg := max(pBar.High, bar.High) - min(pBar.Low, bar.Low)
			rate := float64(0)
			if nerMaxChg != 0 {
				rate = math.Abs(pBar.Close-bar.Close) / nerMaxChg
			}
			jumpRates = append(jumpRates, rate)
		}
		pBar = bar
	}
	chgScore := finScore / float64(totalLen)
	if len(jumpRates) == 0 {
		return chgScore
	}
	// 取平方，扩大分数差距
	jRateScore := math.Pow(1-floats.Sum(jumpRates)/float64(len(jumpRates)), 2)
	return chgScore*0.7 + jRateScore*0.3
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
