package orm

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
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
		return errs.New(core.ErrDbReadFail, err)
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
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s:%s:%s", exchange.Info().ID, market.Type, symbol)
	item, ok := keySymbolMap[key]
	if !ok {
		err := errs.NewMsg(core.ErrInvalidSymbol, "%s not exist in %d cache", symbol, len(keySymbolMap))
		return nil, err
	}
	return item, nil
}

func EnsureExgSymbols(exchange banexg.BanExchange) *errs.Error {
	_, err := LoadMarkets(exchange, false)
	if err != nil {
		return err
	}
	exgId := exchange.Info().ID
	marMap := exchange.GetCurMarkets()
	exsList := make([]*ExSymbol, 0, len(marMap))
	for symbol, market := range marMap {
		exsList = append(exsList, &ExSymbol{Exchange: exgId, Market: market.Type, Symbol: symbol})
	}
	return EnsureSymbols(exsList)
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
		if _, ok := marMap[symbol]; !ok {
			return errs.NewMsg(core.ErrInvalidSymbol, symbol)
		}
		exsList = append(exsList, &ExSymbol{Exchange: exgId, Market: marketType, Symbol: symbol})
	}
	return EnsureSymbols(exsList)
}

func EnsureSymbols(symbols []*ExSymbol) *errs.Error {
	var err *errs.Error
	var exgNames = make(map[string]bool)
	for _, exs := range symbols {
		exgNames[exs.Exchange] = true
	}
	sess, conn, err := Conn(nil)
	if err != nil {
		return err
	}
	defer conn.Release()
	if len(keySymbolMap) == 0 {
		// 尚未加载，加载指定交易所所有标的信息
		for exgId := range exgNames {
			err = sess.LoadExgSymbols(exgId)
			if err != nil {
				return err
			}
		}
	}
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
		}
	}
	if len(adds) == 0 {
		return nil
	}
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
		return errs.New(core.ErrDbExecFail, err_)
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
		return errs.New(core.ErrDbReadFail, err)
	}
	for _, exgId := range exgList {
		err = sess.LoadExgSymbols(exgId)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ExSymbol) GetValidStart(startMS int64) int64 {
	return max(s.ListMs, startMS)
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
				return err
			}
			exs.DelistMs = mar.Expiry
		}
		if oldListMS != exs.ListMs || oldDeListMS != exs.DelistMs {
			err_ := sess.SetListMS(context.Background(), SetListMSParams{
				ID:       exs.ID,
				ListMs:   exs.ListMs,
				DelistMs: exs.DelistMs,
			})
			if err_ != nil {
				return errs.New(core.ErrDbExecFail, err_)
			}
		}
	}
	return nil
}
