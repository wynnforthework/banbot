package strat

import (
	ta "github.com/banbox/banta"
	"sync"
)

/*
下面变量中所有的stratName都是RunPolicy.ID()，不是原始策略名。后面添加了":l"或":s"后缀表示仅开多或仅开空
*/

var (
	Versions    = make(map[string]int)                             // stratName: int 策略版本号
	Envs        = make(map[string]*ta.BarEnv)                      // pair_tf: BarEnv
	AccJobs     = make(map[string]map[string]map[string]*StratJob) // account: pair_tf: [stratID]StratJob
	AccInfoJobs = make(map[string]map[string]map[string]*StratJob) // account: pair_tf: [stratID]StratJob 额外订阅
	PairStrats  = make(map[string]map[string]*TradeStrat)          // pair:[stratID]TradeStrat 所有的订阅策略
	ForbidJobs  = make(map[string]map[string]bool)                 // pair_tf: [stratID] occupy

	BatchTasks  = map[string]*BatchMap{} // tf_account_strat: pair: task 每个bar周期更新（只适用于单交易所单市场）
	LastBatchMS = int64(0)               // timeMS The timestamp of the last batch execution is only used for backtesting 上次批量执行的时间戳，仅用于回测

	lockInfoJobs sync.Mutex

	accOdSubs = map[string][]FnOdChange{} // acc: listeners List of subscription order status change events 订阅订单状态变化事件列表
	lockOdSub sync.Mutex
)
