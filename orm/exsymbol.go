package orm

import (
	"context"
	"fmt"
	"github.com/anyongjin/banbot/config"
	"strings"
)

var (
	keySymbolMap = make(map[string]*ExSymbol)
	idSymbolMap  = make(map[int64]*ExSymbol)
)

func LoadCurSymbols(sess *Queries) error {
	return LoadExgSymbols(sess, config.Exchange.Name, config.MarketType)
}

func LoadExgSymbols(sess *Queries, exgName string, market string) error {
	ctx := context.Background()
	symbols, err := sess.ListSymbols(ctx, ListSymbolsParams{
		Exchange: exgName,
		Market:   market,
	})
	if err != nil {
		return err
	}
	for _, syml := range symbols {
		keySymbolMap[syml.Symbol] = syml
		idSymbolMap[syml.ID] = syml
	}
	return nil
}

func GetSymbolByID(id int64) *ExSymbol {
	item, ok := idSymbolMap[id]
	if !ok {
		return nil
	}
	return item
}

func GetSymbol(exgName string, market string, symbol string) (*ExSymbol, error) {
	key := fmt.Sprintf("%s:%s:%s", exgName, market, symbol)
	item, ok := keySymbolMap[key]
	if !ok {
		return nil, fmt.Errorf("%s not exist in %d cache", symbol, len(keySymbolMap))
	}
	return item, nil
}

func EnsureSymbols(sess *Queries, exgName string, market string, symbols []string) error {
	var items = make([]AddSymbolsParams, len(symbols))
	for i, symbol := range symbols {
		items[i] = AddSymbolsParams{exgName, market, symbol}
	}
	_, err := sess.AddSymbols(context.Background(), items)
	if err != nil {
		return err
	}
	return LoadExgSymbols(sess, exgName, market)
}

func (s *ExSymbol) BaseQuote() (string, string) {
	var arr = strings.Split(s.Symbol, "/")
	if len(arr) != 2 {
		panic(fmt.Sprintf("invalid symbol %s", s.Symbol))
	}
	quote := strings.Split(arr[1], ":")[0]
	return arr[0], quote
}
