package orm

import "github.com/banbox/banexg"

type InOutOrder struct {
	*IOrder
	Enter      *ExOrder
	Exit       *ExOrder
	Info       map[string]interface{}
	DirtyMain  bool   // IOrder 有未保存的临时修改
	DirtyEnter bool   // Enter 有未保存的临时修改
	DirtyExit  bool   // Exit 有未保存的临时修改
	DirtyInfo  bool   // Info 有未保存的临时修改
	idKey      string // 区分订单的key
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
	Factor    float64 // 原始相邻复权因子
	CumFactor float64 // 累计复权因子
	StartMS   int64   // 开始时间
	StopMS    int64   // 结束时间
}

type InfoKline struct {
	*banexg.PairTFKline
	Adj *AdjInfo
}
