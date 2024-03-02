package orm

var (
	OpenODs    = map[int64]*InOutOrder{}    // 全部打开的订单
	HistODs    []*InOutOrder                // 历史订单，回测时作为存储用
	TriggerODs = map[string][]*InOutOrder{} // 等待触发限价单的订单，仅实盘使用
	UpdateOdMs int64                        // 上次刷新OpenODs的时间戳
	FakeOdId   = int64(1)                   // 虚拟订单ID，用于回测时临时维护
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
