package orm

import (
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"reflect"
	"testing"
)

func getExchange(name string, market string, t *testing.T) banexg.BanExchange {
	exchange, err := exg.GetWith(name, market, "")
	if err != nil {
		t.Error(err)
		return nil
	}
	markets, err := LoadMarkets(exchange, false)
	if err != nil {
		t.Error(err)
		return nil
	}
	log.Info("load exchange markets", zap.String("name", name), zap.String("mak", market),
		zap.Int("num", len(markets)))
	err = LoadAllExSymbols()
	if err != nil {
		t.Error(err)
		return nil
	}
	return exchange
}

func TestGetExSymbol(t *testing.T) {
	err := initApp()
	if err != nil {
		panic(err)
	}
	bnb := getExchange("binance", "linear", t)
	if bnb == nil {
		return
	}
	tests := []struct {
		exchange banexg.BanExchange
		symbol   string
		res      *ExSymbol
	}{
		{
			exchange: bnb,
			symbol:   "GAL/USDT:USDT",
			res: &ExSymbol{
				ID:       159,
				Exchange: "binance",
				Market:   "linear",
				Symbol:   "GAL/USDT:USDT",
				ListMs:   1651759200000,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			got, err := GetExSymbol(tt.exchange, tt.symbol)
			if err != nil {
				t.Error(err)
				return
			}
			if !reflect.DeepEqual(got, tt.res) {
				t.Errorf("GetExSymbol() got = %v, want %v", got, tt.res)
			}
		})
	}
}
