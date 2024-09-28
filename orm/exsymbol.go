package orm

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"strings"
	"sync"
)

var (
	keySymbolMap = make(map[string]*ExSymbol)
	idSymbolMap  = make(map[int32]*ExSymbol)
	symbolLock   sync.Mutex
)

func (q *Queries) LoadExgSymbols(exgName string) *errs.Error {
	ctx := context.Background()
	exsList, err := q.ListSymbols(ctx, exgName)
	if err != nil {
		return NewDbErr(core.ErrDbReadFail, err)
	}
	for _, exs := range exsList {
		key := fmt.Sprintf("%s:%s:%s", exs.Exchange, exs.Market, exs.Symbol)
		keySymbolMap[key] = exs
		idSymbolMap[exs.ID] = exs
	}
	return nil
}

func GetExSymbols(exgName, market string) map[int32]*ExSymbol {
	var res = make(map[int32]*ExSymbol)
	for _, exs := range keySymbolMap {
		if exgName != "" && exs.Exchange != exgName {
			continue
		}
		if market != "" && exs.Market != market {
			continue
		}
		res[exs.ID] = exs
	}
	return res
}

func GetSymbolByID(id int32) *ExSymbol {
	item, ok := idSymbolMap[id]
	if !ok {
		return nil
	}
	return item
}

func GetExSymbolCur(symbol string) (*ExSymbol, *errs.Error) {
	return GetExSymbol(exg.Default, symbol)
}

func GetExSymbol(exchange banexg.BanExchange, symbol string) (*ExSymbol, *errs.Error) {
	market, err := exchange.GetMarket(symbol)
	// It is not immediately exited here, it may be delisted, and it is returned empty, but there is historical data, you can try to get it from the cache below
	// 这里不立即退出，可能退市了这里返回空，但有历史数据，可尝试从下面缓存获取
	exgInfo := exchange.Info()
	var marketType = exgInfo.MarketType
	if market != nil {
		marketType = market.Type
	}
	key := fmt.Sprintf("%s:%s:%s", exgInfo.ID, marketType, symbol)
	item, ok := keySymbolMap[key]
	if !ok {
		if err == nil {
			err = errs.NewMsg(core.ErrInvalidSymbol, "%s not exist in %d cache", symbol, len(keySymbolMap))
		}
		return nil, err
	}
	return item, nil
}

func EnsureExgSymbols(exchange banexg.BanExchange) *errs.Error {
	_, err := LoadMarkets(exchange, false)
	if err != nil {
		return err
	}
	exInfo := exchange.Info()
	exgId := exInfo.ID
	marMap := exchange.GetCurMarkets()
	exsList := make([]*ExSymbol, 0, len(marMap))
	for symbol, market := range marMap {
		exsList = append(exsList, &ExSymbol{
			Exchange: exgId,
			Market:   market.Type,
			Symbol:   symbol,
			Combined: market.Combined,
		})
	}
	err = EnsureSymbols(exsList, exgId)
	if err != nil {
		return err
	}
	if len(exInfo.Markets) == 0 {
		// China Futures needs to call LoadMarkets again after EnsureSymbols to pass in symbols for the loading to be successful
		// 中国期货需要在EnsureSymbols后再次调用LoadMarkets传入symbols才能加载成功
		_, err = LoadMarkets(exchange, false)
	} else {
		// Mark the coins that are not returned by the exchange as delisted
		var editList []*ExSymbol
		for _, exs := range idSymbolMap {
			if _, ok := exInfo.Markets[exs.Symbol]; !ok {
				exs.DelistMs = btime.UTCStamp()
				editList = append(editList, exs)
			}
		}
		if len(editList) > 0 {
			ctx := context.Background()
			sess, conn, err := Conn(ctx)
			if err != nil {
				return err
			}
			defer conn.Release()
			for _, exs := range editList {
				err_ := sess.SetListMS(context.Background(), SetListMSParams{
					ID:       exs.ID,
					ListMs:   exs.ListMs,
					DelistMs: exs.DelistMs,
				})
				if err_ != nil {
					return NewDbErr(core.ErrDbExecFail, err_)
				}
			}
		}
	}
	return err
}

func EnsureCurSymbols(symbols []string) *errs.Error {
	exsList := make([]*ExSymbol, 0, len(symbols))
	exgId := config.Exchange.Name
	marketType := core.Market
	marMap, err := LoadMarkets(exg.Default, false)
	if err != nil {
		return err
	}
	for _, symbol := range symbols {
		mar, ok := marMap[symbol]
		if !ok {
			return errs.NewMsg(core.ErrInvalidSymbol, symbol)
		}
		exsList = append(exsList, &ExSymbol{
			Exchange: exgId,
			Market:   marketType,
			Symbol:   symbol,
			Combined: mar.Combined,
		})
	}
	return EnsureSymbols(exsList, exgId)
}

func EnsureSymbols(symbols []*ExSymbol, exchanges ...string) *errs.Error {
	var err *errs.Error
	var exgNames = make(map[string]bool)
	for _, exs := range symbols {
		exgNames[exs.Exchange] = true
	}
	for _, name := range exchanges {
		exgNames[name] = true
	}
	sess, conn, err := Conn(nil)
	if err != nil {
		return err
	}
	defer conn.Release()
	if len(keySymbolMap) == 0 {
		// Not yet loaded, load the information of all the underlying assets of the specified exchange
		// 尚未加载，加载指定交易所所有标的信息
		for exgId := range exgNames {
			err = sess.LoadExgSymbols(exgId)
			if err != nil {
				return err
			}
		}
	}
	// Check the subject matter that needs to be inserted
	// 检查需要插入的标的
	adds := map[string]*ExSymbol{}
	for _, exs := range symbols {
		key := fmt.Sprintf("%s:%s:%s", exs.Exchange, exs.Market, exs.Symbol)
		if item, ok := keySymbolMap[key]; !ok {
			adds[key] = exs
		} else {
			exs.ID = item.ID
			exs.ListMs = item.ListMs
			exs.DelistMs = item.DelistMs
			exs.Combined = item.Combined
		}
	}
	if len(adds) == 0 {
		return nil
	}
	// Lock, reload, and add the data that needs to be added
	// 加锁，重新加载，然后添加需要添加的数据
	symbolLock.Lock()
	defer symbolLock.Unlock()
	for exgId := range exgNames {
		err = sess.LoadExgSymbols(exgId)
		if err != nil {
			return err
		}
	}
	argList := make([]AddSymbolsParams, 0, len(adds))
	for _, item := range adds {
		key := fmt.Sprintf("%s:%s:%s", item.Exchange, item.Market, item.Symbol)
		if _, ok := keySymbolMap[key]; ok {
			continue
		}
		argList = append(argList, AddSymbolsParams{Exchange: item.Exchange, ExgReal: item.ExgReal,
			Market: item.Market, Symbol: item.Symbol})
	}
	_, err_ := sess.AddSymbols(context.Background(), argList)
	if err_ != nil {
		return NewDbErr(core.ErrDbExecFail, err_)
	}
	for exgId := range exgNames {
		err = sess.LoadExgSymbols(exgId)
		if err != nil {
			return err
		}
	}
	// 刷新Sid
	for key, exs := range adds {
		item, _ := keySymbolMap[key]
		if item != nil {
			exs.ID = item.ID
			exs.ListMs = item.ListMs
			exs.DelistMs = item.DelistMs
			exs.Combined = item.Combined
		}
	}
	return nil
}

func LoadAllExSymbols() *errs.Error {
	sess, conn, err := Conn(nil)
	if err != nil {
		return err
	}
	defer conn.Release()
	ctx := context.Background()
	exgList, err_ := sess.ListExchanges(ctx)
	if err_ != nil {
		return NewDbErr(core.ErrDbReadFail, err)
	}
	for _, exgId := range exgList {
		err = sess.LoadExgSymbols(exgId)
		if err != nil {
			return err
		}
	}
	return nil
}

/*
GetAllExSymbols
Gets all the objects that have been loaded into the cache
获取已加载到缓存的所有标的
*/
func GetAllExSymbols() []*ExSymbol {
	res := make([]*ExSymbol, 0, len(idSymbolMap))
	for _, v := range idSymbolMap {
		res = append(res, v)
	}
	return res
}

func (s *ExSymbol) GetValidStart(startMS int64) int64 {
	return max(s.ListMs, startMS)
}

func (s *ExSymbol) ToShort() string {
	slashArr := strings.Split(s.Symbol, "/")
	if len(slashArr) == 1 {
		// 非数字货币，直接返回
		return s.Symbol
	}
	comArr := strings.Split(slashArr[1], ":")
	if len(comArr) == 1 {
		// 现货，直接返回
		return s.Symbol
	}
	base, quote, settle := slashArr[0], comArr[0], comArr[1]
	if !strings.HasPrefix(settle, quote) {
		// 是币本位合约，直接返回
		return s.Symbol
	}
	if quote == settle {
		return fmt.Sprintf("%s/%s.P", base, quote)
	} else {
		suffix := settle[len(quote)+1:]
		return fmt.Sprintf("%s/%s.%s", base, quote, suffix)
	}
}

func InitListDates() *errs.Error {
	ctx := context.Background()
	sess, conn, err := Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	exchange := exg.Default
	exInfo := exchange.Info()
	exsList := GetExSymbols(exInfo.ID, exInfo.MarketType)
	hasFetch := exchange.HasApi(banexg.ApiFetchOHLCV, exInfo.MarketType)
	for _, exs := range exsList {
		var oldListMS, oldDeListMS = exs.ListMs, exs.DelistMs
		if exs.ListMs == 0 {
			startMS := int64(core.MSMinStamp)
			var klines []*banexg.Kline
			if hasFetch {
				klines, err = exchange.FetchOHLCV(exs.Symbol, "1m", startMS, 10, nil)
			} else {
				klines, err = sess.QueryOHLCV(exs.ID, "5m", startMS, 0, 10, true)
			}
			if err != nil {
				return err
			}
			if len(klines) > 0 {
				exs.ListMs = klines[0].Time
			}
		}
		if exs.DelistMs == 0 {
			mar, err := exchange.GetMarket(exs.Symbol)
			if err != nil {
				if err.Code != errs.CodeNoMarketForPair {
					return err
				}
			} else {
				exs.DelistMs = mar.Expiry
			}
		}
		if oldListMS != exs.ListMs || oldDeListMS != exs.DelistMs {
			err_ := sess.SetListMS(context.Background(), SetListMSParams{
				ID:       exs.ID,
				ListMs:   exs.ListMs,
				DelistMs: exs.DelistMs,
			})
			if err_ != nil {
				return NewDbErr(core.ErrDbExecFail, err_)
			}
		}
	}
	return nil
}

func ParseShort(exgName, short string) (*ExSymbol, *errs.Error) {
	slashArr := strings.Split(short, "/")
	var symbol string
	var market = banexg.MarketSpot
	if len(slashArr) > 1 {
		// 对数字货币 BTC/USDT:USDT BTC/USDT.P BTC/USDT.2309
		dotArr := strings.Split(slashArr[1], ".")
		quote := dotArr[0]
		var suffix = ""
		if len(dotArr) > 1 {
			suffix = quote
			market = banexg.MarketLinear
			if !strings.EqualFold(dotArr[1], "p") {
				suffix += "-" + dotArr[1]
			}
		} else {
			comArr := strings.Split(quote, ":")
			if len(comArr) > 1 {
				quote, suffix = comArr[0], comArr[1]
				if strings.HasPrefix(suffix, quote) {
					market = banexg.MarketLinear
				} else {
					market = banexg.MarketInverse
				}
			}
		}
		if market == banexg.MarketSpot {
			symbol = fmt.Sprintf("%s/%s", slashArr[0], quote)
		} else {
			symbol = fmt.Sprintf("%s/%s:%s", slashArr[0], quote, suffix)
		}
	} else {
		symbol = short
	}
	key := fmt.Sprintf("%s:%s:%s", exgName, market, symbol)
	item, ok := keySymbolMap[key]
	if !ok {
		err := errs.NewMsg(core.ErrInvalidSymbol, "%s not exist in %d cache", symbol, len(keySymbolMap))
		return nil, err
	}
	return item, nil
}
