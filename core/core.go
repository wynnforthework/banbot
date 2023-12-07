package core

import (
	"fmt"
	"strings"
)

var (
	barPrices = make(map[string]float64) //# 来自bar的每个币的最新价格，仅用于回测等。键可以是交易对，也可以是币的code
	prices    = make(map[string]float64) //交易对的最新订单簿价格，仅用于实时模拟或实盘。键可以是交易对，也可以是币的code
)

func GetPrice(symbol string) float64 {
	if price, ok := prices[symbol]; ok {
		return price
	}
	if price, ok := barPrices[symbol]; ok {
		return price
	}
	if strings.Contains(symbol, "USD") && strings.Contains(symbol, "/") {
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
