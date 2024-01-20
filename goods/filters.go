package goods

import (
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"gonum.org/v1/gonum/stat"
	"math"
	"math/rand"
	"slices"
)

func (f *BaseFilter) IsNeedTickers() bool {
	return f.NeedTickers
}

func (f *BaseFilter) IsEnable() bool {
	return f.Enable
}

func (f *BaseFilter) GetName() string {
	return f.Name
}

func (f *AgeFilter) Filter(symbols []string, tickers map[string]*banexg.Ticker) ([]string, *errs.Error) {
	if f.Min == 0 && f.Max == 0 {
		return symbols, nil
	}
	backNum := max(f.Max, f.Min) + 1
	return filterByOHLCV(symbols, "1d", backNum, func(s string, klines []*banexg.Kline) bool {
		knum := len(klines)
		return knum >= f.Max && (f.Max == 0 || knum <= f.Max)
	})
}

func (f *VolumePairFilter) Filter(symbols []string, tickers map[string]*banexg.Ticker) ([]string, *errs.Error) {
	var symbolVols = make([]SymbolVol, 0)
	if !f.NeedTickers {
		startMS := int64(0)
		if !core.LiveMode() {
			startMS = config.TimeRange.StartMS
		}
		limit := f.BackPeriod
		callBack := func(symbol string, _ string, klines []*banexg.Kline) {
			if len(klines) == 0 {
				symbolVols = append(symbolVols, SymbolVol{symbol, 0})
			} else {
				total := float64(0)
				slices.Reverse(klines)
				for _, k := range klines[:f.BackPeriod] {
					total += k.Close * k.Volume
				}
				symbolVols = append(symbolVols, SymbolVol{symbol, total})
			}
		}
		exchange, err := exg.Get()
		if err != nil {
			return nil, err
		}
		err = orm.FastBulkOHLCV(exchange, symbols, f.BackTimeframe, startMS, 0, limit, callBack)
		if err != nil {
			return nil, err
		}
	} else {
		for _, symbol := range symbols {
			tik, ok := tickers[symbol]
			if !ok {
				continue
			}
			symbolVols = append(symbolVols, SymbolVol{symbol, tik.QuoteVolume})
		}
	}
	slices.SortFunc(symbolVols, func(a, b SymbolVol) int {
		return int((b.Vol - a.Vol) / 1000)
	})
	if f.MinValue > 0 {
		cutLen := len(symbolVols)
		for i, v := range symbolVols {
			if v.Vol < f.MinValue {
				continue
			}
			cutLen = i
			break
		}
		symbolVols = symbolVols[:cutLen]
	}
	if f.Limit > len(symbolVols) {
		symbolVols = symbolVols[:f.Limit]
	}
	resPairs := make([]string, len(symbolVols))
	for i, v := range symbolVols {
		resPairs[i] = v.Symbol
	}
	return resPairs, nil
}

type SymbolVol struct {
	Symbol string
	Vol    float64
}

func (f *VolumePairFilter) GenSymbols(tickers map[string]*banexg.Ticker) ([]string, *errs.Error) {
	symbols := make([]string, 0)
	if f.NeedTickers && len(tickers) > 0 {
		for symbol, _ := range tickers {
			symbols = append(symbols, symbol)
		}
	} else {
		exchange, err := exg.Get()
		if err != nil {
			return nil, err
		}
		markets := exchange.GetCurMarkets()
		symbols = utils.KeysOfMap(markets)
	}
	return f.Filter(symbols, tickers)
}

func (f *PriceFilter) Filter(symbols []string, tickers map[string]*banexg.Ticker) ([]string, *errs.Error) {
	if core.LiveMode() {
		res := make([]string, 0, len(symbols))
		if len(tickers) == 0 {
			log.Warn("no tickers, PriceFilter skipped")
			return symbols, nil
		}
		for _, s := range symbols {
			tik, ok := tickers[s]
			if !ok {
				continue
			}
			if f.validatePrice(s, tik.Last) {
				res = append(res, s)
			}
		}
		return res, nil
	} else {
		return filterByOHLCV(symbols, "1h", 1, func(s string, klines []*banexg.Kline) bool {
			if len(klines) == 0 {
				return false
			}
			return f.validatePrice(s, klines[0].Close)
		})
	}
}

func (f *PriceFilter) validatePrice(symbol string, price float64) bool {
	exchange, err := exg.Get()
	if err != nil {
		log.Error("get exchange fail, price validate skip")
		return true
	}
	if f.Precision > 0 {
		pip, err := exchange.PriceOnePip(symbol)
		if err != nil {
			log.Error("get one pip of price fail", zap.String("symbol", symbol))
			return false
		}
		chgPrec := pip / price
		if chgPrec > f.Precision {
			log.Info("PriceFilter drop, 1 unit fail", zap.String("pair", symbol), zap.Float64("p", chgPrec))
			return false
		}
	}

	if f.MaxUnitValue > 0 {
		market, err := exchange.GetMarket(symbol)
		if err != nil {
			log.Error("PriceFilter drop, market not exist", zap.String("pair", symbol))
			return false
		}
		minPrec := market.Precision.Amount
		if minPrec > 0 {
			if exchange.PrecMode() != banexg.PrecModeTickSize {
				minPrec = math.Pow(0.1, minPrec)
			}
			unitVal := minPrec * price
			if unitVal > f.MaxUnitValue {
				log.Info("PriceFilter drop, unit value too small", zap.String("pair", symbol),
					zap.Float64("uv", unitVal))
				return false
			}
		}
	}

	if f.MinPrice > 0 && price < f.MinPrice {
		log.Info("PriceFilter drop, price too small", zap.String("pair", symbol), zap.Float64("price", price))
		return false
	}

	if f.MaxPrice > price {
		log.Info("PriceFilter drop, price too big", zap.String("pair", symbol), zap.Float64("price", price))
		return false
	}
	return true
}

func (f *RateOfChangeFilter) Filter(symbols []string, tickers map[string]*banexg.Ticker) ([]string, *errs.Error) {
	return filterByOHLCV(symbols, "1d", f.BackDays, f.validate)
}

func (f *RateOfChangeFilter) validate(pair string, arr []*banexg.Kline) bool {
	if len(arr) == 0 {
		return false
	}
	hhigh := arr[0].High
	llow := arr[0].Low
	for _, k := range arr[1:] {
		hhigh = max(hhigh, k.High)
		llow = min(llow, k.Low)
	}
	roc := float64(0)
	if llow > 0 {
		roc = (hhigh - llow) / llow
	}
	if f.Min > roc {
		log.Info("RateOfChangeFilter drop by min", zap.String("pair", pair), zap.Float64("roc", roc))
		return false
	}
	if f.Max > 0 && f.Max < roc {
		log.Info("RateOfChangeFilter drop by max", zap.String("pair", pair), zap.Float64("roc", roc))
		return false
	}
	return true
}

func filterByOHLCV(symbols []string, timeFrame string, limit int, cb func(string, []*banexg.Kline) bool) ([]string, *errs.Error) {
	exchange, err := exg.Get()
	if err != nil {
		return nil, err
	}
	var res []string
	handle := func(pair string, _ string, arr []*banexg.Kline) {
		if cb(pair, arr) {
			res = append(res, pair)
		}
	}
	err = orm.FastBulkOHLCV(exchange, symbols, timeFrame, 0, 0, limit, handle)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (f *VolatilityFilter) Filter(symbols []string, tickers map[string]*banexg.Ticker) ([]string, *errs.Error) {
	return filterByOHLCV(symbols, "1d", f.BackDays, func(s string, klines []*banexg.Kline) bool {
		var logRates = make([]float64, 0, len(klines))
		var weights = make([]float64, 0, len(klines))
		for i, v := range klines[1:] {
			pc := klines[i].Close
			logRate := math.Log(v.Close / pc)
			logRates = append(logRates, logRate)
			weights = append(weights, 1)
		}
		logRates = append(logRates, 0)
		weights = append(weights, 1)
		stdDev := stat.StdDev(logRates, weights)
		res := stdDev * math.Sqrt(float64(len(klines)))
		if res < f.Min || f.Max > 0 && res > f.Max {
			log.Info("VolatilityFilter drop", zap.String("pair", s), zap.Float64("v", res))
			return false
		}
		return true
	})
}

func (f *SpreadFilter) Filter(symbols []string, tickers map[string]*banexg.Ticker) ([]string, *errs.Error) {
	return nil, nil
}

func (f *OffsetFilter) Filter(symbols []string, tickers map[string]*banexg.Ticker) ([]string, *errs.Error) {
	res := symbols[f.Offset:]
	if f.Limit > 0 {
		res = res[:f.Limit]
	}
	if len(res) < len(symbols) {
		log.Info("OffsetFilter res", zap.Int("len", len(res)))
	}
	return res, nil
}

func (f *ShuffleFilter) Filter(symbols []string, tickers map[string]*banexg.Ticker) ([]string, *errs.Error) {
	rand.Shuffle(len(symbols), func(i, j int) {
		symbols[i], symbols[j] = symbols[j], symbols[i]
	})
	return symbols, nil
}
