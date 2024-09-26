package orm

import "sync"

var (
	accOpenODs     = map[string]map[int64]*InOutOrder{}            // 全部打开的订单；accName:orderId:order
	accTriggerODs  = map[string]map[string]map[int64]*InOutOrder{} // 等待触发限价单的订单，仅实盘使用；accName:pair:orders
	lockOpenMap    = map[string]*sync.Mutex{}                      // 访问accOpenODs的锁
	lockTriggerMap = map[string]*sync.Mutex{}                      // 访问accTriggerODs的锁
	lockOds        = map[string]*sync.Mutex{}                      // 修改订单的锁，防止并发修改
	mLockOds       sync.Mutex
	HistODs        []*InOutOrder // 历史订单，回测时作为存储用
	FakeOdId       = int64(1)    // 虚拟订单ID，用于回测时临时维护

	adjMap = map[int32][]*AdjInfo{} // 标的的复权因子缓存
	amLock = sync.Mutex{}
)

const (
	InOutStatusInit = iota
	InOutStatusPartEnter
	InOutStatusFullEnter
	InOutStatusPartExit
	InOutStatusFullExit
	InOutStatusDelete
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
	OdInfoLegalCost = "LegalCost"
	OdInfoStopAfter = "StopAfter"
)

const (
	OdActionEnter      = "Enter"
	OdActionExit       = "Exit"
	OdActionLimitEnter = "LimitEnter"
	OdActionLimitExit  = "LimitExit"
	OdActionStopLoss   = "StopLoss"
	OdActionTakeProfit = "TakeProfit"
)
