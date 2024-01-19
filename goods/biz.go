package goods

import (
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/base"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/dgraph-io/ristretto"
	"github.com/mitchellh/mapstructure"
	"go.uber.org/zap"
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
	var tickersMap map[string]*base.Ticker
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
				tickersMap, _ = tikCache.(map[string]*base.Ticker)
			}
		}
		pairs, err = pairProducer.GenSymbols(tickersMap)
		if err != nil {
			return err
		}
		log.Info("gen symbols", zap.String("from", pairProducer.GetName()),
			zap.Int("num", len(pairs)))
	}
	pairs = utils.UnionArr(pairs, addPairs, config.GetExgConfig().WhitePairs)
	err = orm.EnsureCurSymbols(pairs)
	if err != nil {
		return err
	}
	if allowFilter {
		for _, flt := range filters {
			if !flt.IsEnable() {
				continue
			}
			pairs, err = flt.Filter(pairs, tickersMap)
			if err != nil {
				return err
			}
			log.Info("left symbols", zap.String("after", flt.GetName()), zap.Int("num", len(pairs)))
		}
	}
	// 计算交易对各维度K线质量分数
	return nil
}
