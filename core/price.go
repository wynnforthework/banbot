package core

import (
	"fmt"
	"github.com/banbox/banexg"
	"strings"
)

func GetPriceSafe(symbol string) float64 {
	if IsFiat(symbol) && !strings.Contains(symbol, "/") {
		return 1
	}
	lockPrices.RLock()
	price, ok := prices[symbol]
	lockPrices.RUnlock()
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

func GetPrice(symbol string) float64 {
	price := GetPriceSafe(symbol)
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
	empty := len(prices) == 0 && len(barPrices) == 0
	lockBarPrices.RUnlock()
	lockPrices.RUnlock()
	return empty
}

func SetPrices(data map[string]float64) {
	lockPrices.Lock()
	for pair, price := range data {
		prices[pair] = price
		base, quote, settle, _ := SplitSymbol(pair)
		if IsFiat(quote) && (settle == "" || settle == quote) {
			prices[base] = price
		}
	}
	lockPrices.Unlock()
}

func IsMaker(pair, side string, price float64) bool {
	curPrice := GetPrice(pair)
	isBuy := side == banexg.OdSideBuy
	isLow := price < curPrice
	return isBuy == isLow
}
