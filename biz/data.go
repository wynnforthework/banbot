package biz

var (
	barPrices = make(map[string]float64) //# 来自bar的每个币的最新价格，仅用于回测等。键可以是交易对，也可以是币的code
	prices    = make(map[string]float64) //交易对的最新订单簿价格，仅用于实时模拟或实盘。键可以是交易对，也可以是币的code

)
