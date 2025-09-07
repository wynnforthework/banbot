package core

import (
	"fmt"
	"github.com/banbox/banexg"
	"github.com/sasha-s/go-deadlock"
	"gonum.org/v1/gonum/floats"
	"strings"
)

func getPriceBySide(ask, bid map[string]float64, lock *deadlock.RWMutex, symbol string, side string) (float64, bool) {
	lock.RLock()
	priceArr := make([]float64, 0, 1)
	if side == banexg.OdSideBuy || side == "" {
		if price, ok := bid[symbol]; ok {
			priceArr = append(priceArr, price)
		}
	}
	if side == banexg.OdSideSell || side == "" {
		if price, ok := ask[symbol]; ok {
			priceArr = append(priceArr, price)
		}
	}
	lock.RUnlock()
	if len(priceArr) > 0 {
		if len(priceArr) == 1 {
			return priceArr[0], true
		}
		return floats.Sum(priceArr) / float64(len(priceArr)), true
	}
	return 0, false
}

func GetPriceSafe(symbol string, side string) float64 {
	if IsFiat(symbol) && !strings.Contains(symbol, "/") {
		return 1
	}
	price, ok := getPriceBySide(askPrices, bidPrices, &lockPrices, symbol, side)
	if ok {
		return price
	}
	lockBarPrices.RLock()
	price, ok = barPrices[symbol]
	lockBarPrices.RUnlock()
	if ok {
		return price
	}
	return -1
}

func GetPrice(symbol string, side string) float64 {
	price := GetPriceSafe(symbol, side)
	if price == -1 {
		panic(fmt.Errorf("invalid symbol for price: %s", symbol))
	}
	return price
}

func setDataPrice(data map[string]float64, pair string, price float64) {
	data[pair] = price
	base, quote, settle, _ := SplitSymbol(pair)
	if IsFiat(quote) && (settle == "" || settle == quote) {
		data[base] = price
	}
}

func SetBarPrice(pair string, price float64) {
	lockBarPrices.Lock()
	setDataPrice(barPrices, pair, price)
	lockBarPrices.Unlock()
}

func IsPriceEmpty() bool {
	lockPrices.RLock()
	lockBarPrices.RLock()
	empty := len(bidPrices) == 0 && len(barPrices) == 0
	lockBarPrices.RUnlock()
	lockPrices.RUnlock()
	return empty
}

func SetPrice(pair string, ask, bid float64) {
	lockPrices.Lock()
	if ask > 0 {
		askPrices[pair] = ask
	}
	if bid > 0 {
		bidPrices[pair] = ask
	}
	lockPrices.Unlock()
}

func SetPrices(data map[string]float64, side string) {
	updateAsk := side == banexg.OdSideSell || side == ""
	updateBid := side == banexg.OdSideBuy || side == ""
	if !updateBid && !updateAsk {
		panic(fmt.Sprintf("invalid side: %v, use `banexg.OdSideBuy/OdSideSell` or ''", side))
	}
	lockPrices.Lock()
	for pair, price := range data {
		if updateAsk {
			askPrices[pair] = price
		}
		if updateBid {
			bidPrices[pair] = price
		}
		base, quote, settle, _ := SplitSymbol(pair)
		if IsFiat(quote) && (settle == "" || settle == quote) {
			if updateAsk {
				askPrices[base] = price
			}
			if updateBid {
				bidPrices[base] = price
			}
		}
	}
	lockPrices.Unlock()
}

func IsMaker(pair, side string, price float64) bool {
	curPrice := GetPrice(pair, side)
	isBuy := side == banexg.OdSideBuy
	isLow := price < curPrice
	return isBuy == isLow
}
