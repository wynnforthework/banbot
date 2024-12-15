package biz

import (
	"fmt"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg"
	"math"
	"testing"
)

/*
*
ETH/USDT:USDT current price: 3426.9050
buy at 3769.5955 would cost at least 0 secs to fill 100.0%
buy at 3423.4781 would cost at least 112 secs to fill 100.0%
buy at 3420.0512 would cost at least 252 secs to fill 100.0%
buy at 3409.7705 would cost at least 525 secs to fill 71.0%
buy at 3392.6359 would cost at least 525 secs to fill 35.5%
buy at 3324.0978 would cost at least 525 secs to fill 11.8%
buy at 3255.5597 would cost at least 525 secs to fill 7.1%
buy at 3084.2145 would cost at least 525 secs to fill 3.5%
*/
func TestCalcSecsForPrice(t *testing.T) {
	err := initApp()
	if err != nil {
		panic(err)
	}
	_, err = orm.LoadMarkets(exg.Default, false)
	if err != nil {
		panic(err)
	}
	pair := "ETH/USDT:USDT"
	side := banexg.OdSideBuy
	priceRates := []float64{-0.1, 0.001, 0.002, 0.005, 0.01, 0.03, 0.05, 0.1}
	err = orm.EnsureCurSymbols([]string{pair})
	if err != nil {
		panic(err)
	}
	avgVol, lastVol, err := getPairMinsVol(pair, 50)
	if err != nil {
		panic(err)
	}
	secsVol := max(avgVol, lastVol) / 60
	if secsVol == 0 {
		panic(err)
	}
	book, err := exg.GetOdBook(pair)
	if err != nil {
		panic(err)
	}
	curPrice := book.Asks.Price[0]*0.5 + book.Bids.Price[0]*0.5
	fmt.Printf("%s current price: %.4f \n", pair, curPrice)
	dirt := float64(1)
	if side == banexg.OdSideBuy {
		dirt = float64(-1)
	}
	for _, rate := range priceRates {
		price := curPrice * (1 + rate*dirt)
		waitVol, rate := book.SumVolTo(side, price)
		minWaitSecs := int(math.Round(waitVol / secsVol))
		fmt.Printf("%s at %.4f would cost at least %d secs to fill %.1f%%\n",
			side, price, minWaitSecs, rate*100)
	}
}
