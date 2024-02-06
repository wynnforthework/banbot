package orm

import (
	"context"
	"fmt"
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
	exchange, err := exg.Get()
	if err != nil {
		return nil, err
	}
	return GetExSymbol(exchange, symbol)
}

func GetExSymbol(exchange banexg.BanExchange, symbol string) (*ExSymbol, *errs.Error) {
	market, err := exchange.GetMarket(symbol)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s:%s:%s", exchange.GetID(), market.Type, symbol)
	item, ok := keySymbolMap[key]
	if !ok {
		err := errs.NewMsg(core.ErrInvalidSymbol, "%s not exist in %d cache", symbol, len(keySymbolMap))
		return nil, err
	}
	return item, nil
}

func EnsureExgSymbols(exchange banexg.BanExchange) *errs.Error {
	_, err := exchange.LoadMarkets(false, nil)
	if err != nil {
		return err
	}
	exgId := exchange.GetID()
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
	for _, symbol := range symbols {
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
		for exgId := range exgNames {
			err = sess.LoadExgSymbols(exgId)
			if err != nil {
				return err
			}
		}
	}
	adds := map[string]*ExSymbol{}
	for _, exs := range symbols {
		key := fmt.Sprintf("%s:%s:%s", exs.Exchange, exs.Market, exs.Symbol)
		if _, ok := keySymbolMap[key]; !ok {
			adds[key] = exs
		}
	}
	if len(adds) == 0 {
		return nil
	}
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
		argList = append(argList, AddSymbolsParams{Exchange: item.Exchange, Market: item.Market, Symbol: item.Symbol})
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
	return nil
}

func (s *ExSymbol) BaseQuote() (string, string) {
	var arr = strings.Split(s.Symbol, "/")
	if len(arr) != 2 {
		panic(fmt.Sprintf("invalid symbol %s", s.Symbol))
	}
	quote := strings.Split(arr[1], ":")[0]
	return arr[0], quote
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
	for _, exs := range idSymbolMap {
		if exs.ListMs > 0 {
			continue
		}
		exchange, err := exg.GetWith(exs.Exchange, exs.Market, "")
		if err != nil {
			return err
		}
		klines, err := exchange.FetchOHLCV(exs.Symbol, "1m", core.MSMinStamp, 10, nil)
		if err != nil {
			return err
		}
		if len(klines) > 0 {
			exs.ListMs = klines[0].Time
			err_ := sess.SetListMS(context.Background(), SetListMSParams{
				ID:       int64(exs.ID),
				ListMs:   klines[0].Time,
				DelistMs: 0,
			})
			if err_ != nil {
				return errs.New(core.ErrDbExecFail, err_)
			}
		}
	}
	return nil
}
