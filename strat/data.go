package strat

import (
	ta "github.com/banbox/banta"
	"sync"
)

/*
下面变量中所有的stagyName都是RunPolicy.ID()，不是原始策略名。后面添加了":l"或":s"后缀表示仅开多或仅开空
*/

var (
	Versions    = make(map[string]int)                             // stagyName: int 策略版本号
	Envs        = make(map[string]*ta.BarEnv)                      // pair_tf: BarEnv
	AccJobs     = make(map[string]map[string]map[string]*StagyJob) // account: pair_tf: [stagyName]StagyJob
	AccInfoJobs = make(map[string]map[string]map[string]*StagyJob) // account: pair_tf: [stagyName]StagyJob 额外订阅
	PairStags   = make(map[string]map[string]*TradeStagy)          // pair:[stagyName]TradeStagy 所有的订阅策略

	BatchTasks  = map[string]*BatchMap{} // tf_account_stagy: pair: task 每个bar周期更新（只适用于单交易所单市场）
	LastBatchMS = int64(0)               // timeMS The timestamp of the last batch execution is only used for backtesting 上次批量执行的时间戳，仅用于回测

	lockInfoJobs sync.Mutex

	accOdSubs = map[string][]FnOdChange{} // acc: listeners List of subscription order status change events 订阅订单状态变化事件列表
	lockOdSub sync.Mutex
)
