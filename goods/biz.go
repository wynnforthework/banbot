package goods

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/mitchellh/mapstructure"
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
	fts, err := GetPairFilters(config.PairFilters, false)
	if err != nil {
		return err
	}
	for i, flt := range fts {
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
	filters = fts[1:]
	return nil
}

func GetPairFilters(items []*config.CommonPairFilter, withInvalid bool) ([]IFilter, *errs.Error) {
	fts := make([]IFilter, 0, len(items))
	for _, cfg := range items {
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
			return nil, errs.NewMsg(errs.CodeParamInvalid, "unknown symbol filter: %s", cfg.Name)
		}
		err_ := mapstructure.Decode(cfg.Items, &output)
		if err_ != nil {
			return nil, errs.New(errs.CodeUnmarshalFail, err_)
		}
		if withInvalid || !output.IsDisable() {
			fts = append(fts, output)
		}
	}
	return fts, nil
}

func RefreshPairList() ([]string, *errs.Error) {
	lastRefresh = btime.TimeMS()
	var pairs []string
	var allowFilter = false
	var err *errs.Error
	var tickersMap map[string]*banexg.Ticker
	if len(config.Pairs) > 0 {
		pairs = config.Pairs
	} else {
		allowFilter = true
		if needTickers && core.LiveMode {
			tickersMap, err = exg.GetTickers()
			if err != nil {
				return nil, err
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
	// 数量和偏移限制
	mgrCfg := config.PairMgr
	if mgrCfg.Offset > 0 {
		if mgrCfg.Offset < len(pairs) {
			pairs = pairs[mgrCfg.Offset:]
		} else {
			pairs = nil
		}
	}
	if mgrCfg.Limit > 0 && mgrCfg.Limit < len(pairs) {
		pairs = pairs[:mgrCfg.Limit]
	}

	core.Pairs = nil
	core.PairsMap = make(map[string]bool)
	for _, p := range pairs {
		core.Pairs = append(core.Pairs, p)
		core.PairsMap[p] = true
	}
	return pairs, nil
}
