package core

import (
	"fmt"
	"github.com/banbox/banexg"
	"strings"
)

func GetPrice(symbol string) float64 {
	if strings.Contains(symbol, "USD") && !strings.Contains(symbol, "/") {
		return 1
	}
	if price, ok := prices[symbol]; ok {
		return price
	}
	if price, ok := barPrices[symbol]; ok {
		return price
	}
	panic(fmt.Errorf("invalid symbol for price: %s", symbol))
}

func setDataPrice(data map[string]float64, pair string, price float64) {
	data[pair] = price
	base, quote, settle, _ := SplitSymbol(pair)
	if strings.Contains(quote, "USD") && (settle == "" || settle == quote) {
		data[base] = price
	}
}

func SetBarPrice(pair string, price float64) {
	setDataPrice(barPrices, pair, price)
}

func SetPrice(pair string, price float64) {
	setDataPrice(prices, pair, price)
}

func IsPriceEmpty() bool {
	return len(prices) == 0 && len(barPrices) == 0
}

func SetPrices(data map[string]float64) {
	for pair, price := range data {
		prices[pair] = price
		base, quote, settle, _ := SplitSymbol(pair)
		if strings.Contains(quote, "USD") && (settle == "" || settle == quote) {
			prices[base] = price
		}
	}
}

func IsMaker(pair, side string, price float64) bool {
	curPrice := GetPrice(pair)
	isBuy := side == banexg.OdSideBuy
	isLow := price < curPrice
	return isBuy == isLow
}
