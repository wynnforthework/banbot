package orm

import "github.com/banbox/banexg"

type InOutOrder struct {
	*IOrder
	Enter      *ExOrder
	Exit       *ExOrder
	Info       map[string]interface{}
	DirtyMain  bool   // IOrder has unsaved temporary changes 有未保存的临时修改
	DirtyEnter bool   // Enter has unsaved temporary changes 有未保存的临时修改
	DirtyExit  bool   // Exit has unsaved temporary changes 有未保存的临时修改
	DirtyInfo  bool   // Info has unsaved temporary changes 有未保存的临时修改
	idKey      string // Key to distinguish orders 区分订单的key
}

type InOutEdit struct {
	Order  *InOutOrder
	Action string
}

type KlineAgg struct {
	TimeFrame string
	MSecs     int64
	Table     string
	AggFrom   string
	AggStart  string
	AggEnd    string
	AggEvery  string
	CpsBefore string
	Retention string
}

type AdjInfo struct {
	*ExSymbol
	Factor    float64 // Original adjacent weighting factor 原始相邻复权因子
	CumFactor float64 // Cumulative weighting factor 累计复权因子
	StartMS   int64   // start timestamp 开始时间
	StopMS    int64   // stop timestamp 结束时间
}

type InfoKline struct {
	*banexg.PairTFKline
	Adj      *AdjInfo
	IsWarmUp bool
}

type ExitTrigger struct {
	Price float64 `json:"price"` // Trigger Price 触发价格
	Limit float64 `json:"limit"` // Submit limit order price after triggering, otherwise market order. 触发后提交限价单价格，否则市价单
	Rate  float64 `json:"rate"`  // Stop-profit and stop-loss ratio, (0,1], 0 means all. 止盈止损比例，(0,1]，0表示全部
	Tag   string  `json:"tag"`   // Reason, used for ExitTag. 原因，用于ExitTag
}

type TriggerState struct {
	*ExitTrigger
	Range   float64 `json:"range"` // The stop-profit and stop-loss range is the range from the entry price to the exit price. 止盈止损区间，入场价格到离场价格的区间
	Hit     bool    `json:"hit"`   // whether trigger price has been triggered? 是否已触发
	OrderId string  `json:"order_id"`
	Old     *ExitTrigger
}
