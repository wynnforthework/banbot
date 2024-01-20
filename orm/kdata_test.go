package orm

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banexg"
	"testing"
)

func TestAutoFetchOhlcv(t *testing.T) {
	err := initApp()
	if err != nil {
		panic(err)
	}
	exchange, err := exg.Get()
	if err != nil {
		panic(err)
	}
	// 交易对初始化
	err = EnsureExgSymbols(exchange)
	if err != nil {
		panic(err)
	}
	err = InitListDates()
	if err != nil {
		panic(err)
	}
	exs, err := GetSymbol(exchange.GetID(), banexg.MarketLinear, "BTC/USDT:USDT")
	if err != nil {
		panic(err)
	}
	stop := btime.TimeMS()
	start := int64(0)
	klines, err := AutoFetchOHLCV(exchange, exs, "1m", start, stop, 1000, false)
	if err != nil {
		panic(err)
	}
	for _, k := range klines {
		fmt.Printf("%v %f %f %f %f %f\n", k.Time, k.Open, k.High, k.Low, k.Close, k.Volume)
	}
}
