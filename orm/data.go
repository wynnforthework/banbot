package orm

var (
	AccOpenODs    = map[string]map[int64]*InOutOrder{}    // 全部打开的订单；accName:orderId:order
	AccTriggerODs = map[string]map[string][]*InOutOrder{} // 等待触发限价单的订单，仅实盘使用；accName:pair:orders
	HistODs       []*InOutOrder                           // 历史订单，回测时作为存储用
	FakeOdId      = int64(1)                              // 虚拟订单ID，用于回测时临时维护
)

const (
	InOutStatusInit = iota
	InOutStatusPartEnter
	InOutStatusFullEnter
	InOutStatusPartExit
	InOutStatusFullExit
)

const (
	OdStatusInit = iota
	OdStatusPartOK
	OdStatusClosed
)

const (
	KeyStatusMsg = "status_msg"
)

const (
	OdInfoStopLoss          = "StopLossPrice"
	OdInfoStopLossOrderId   = "StopLossOrderId"
	OdInfoTakeProfit        = "TakeProfitPrice"
	OdInfoTakeProfitOrderId = "TakeProfitOrderId"
	OdInfoLegalCost         = "LegalCost"
	OdInfoStopAfter         = "StopAfter"
)
