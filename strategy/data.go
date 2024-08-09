package strategy

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

	BatchJobs   = map[string]map[string]*StagyJob{} // tf_account_stagy: pair: job 每个bar周期更新
	BatchInfos  = map[string]map[string]*StagyJob{} // tf_account_stagy: pair: job 每个info bar周期更新
	TFEnterMS   = map[string]int64{}                // tf: timeMS 执行入场的时间戳
	TFInfoMS    = map[string]int64{}                // tf: timeMS 执行Info的时间戳
	LastBatchMS = map[string]int64{}                // tf: timeMS 仅用于回测

	lockInfoJobs sync.Mutex

	accOdSubs = map[string][]FnOdChange{} // acc: listeners 订阅订单状态变化事件列表
	lockOdSub sync.Mutex
)
