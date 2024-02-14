package core

import (
	"fmt"
	"github.com/banbox/banexg"
	"strings"
)

func GetPrice(symbol string) float64 {
	if price, ok := prices[symbol]; ok {
		return price
	}
	if price, ok := barPrices[symbol]; ok {
		return price
	}
	if strings.Contains(symbol, "USD") && !strings.Contains(symbol, "/") {
		return 1
	}
	panic(fmt.Errorf("invalid symbol for price: %s", symbol))
}

func setDataPrice(data map[string]float64, pair string, price float64) {
	data[pair] = price
	if strings.Contains(pair, "USD") {
		data[strings.Split(pair, "/")[0]] = price
	}
}

func SetBarPrice(pair string, price float64) {
	setDataPrice(barPrices, pair, price)
}

func SetPrice(pair string, price float64) {
	setDataPrice(prices, pair, price)
}

func IsMaker(pair, side string, price float64) bool {
	curPrice := GetPrice(pair)
	isBuy := side == banexg.OdSideBuy
	isLow := price < curPrice
	return isBuy == isLow
}
