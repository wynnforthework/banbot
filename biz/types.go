package biz

type ItemWallet struct {
	Coin          string             // 币代码，非交易对
	Available     float64            //可用余额
	Pendings      map[string]float64 //买入卖出时锁定金额，键可以是订单id
	Frozens       map[string]float64 //空单等长期冻结金额，键可以是订单id
	UnrealizedPOL float64            //此币的公共未实现盈亏，合约用到，可抵扣其他订单保证金占用。每个bar重新计算
	UsedUPol      float64            //已占用的未实现盈亏（用作其他订单的保证金）
	Withdraw      float64            //从余额提现的，不会用于交易
}

type Wallets struct {
	Items         map[string]*ItemWallet
	MarginAddRate float64 //出现亏损时，在亏损百分比后追加保证金
	MinOpenRate   float64 //钱包余额不足单笔开单金额时，超过此比例即允许开小单
}
