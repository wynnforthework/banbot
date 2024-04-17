package strategy

import (
	ta "github.com/banbox/banta"
)

var (
	Versions        = make(map[string]int)                             // 策略版本号
	Envs            = make(map[string]*ta.BarEnv)                      // pair_tf: BarEnv
	AccJobs         = make(map[string]map[string]map[string]*StagyJob) // account: pair_tf: [stagyName]StagyJob
	AccInfoJobs     = make(map[string]map[string]map[string]*StagyJob) // account: pair_tf: [stagyName]StagyJob 额外订阅
	PairTFStags     = make(map[string]map[string]*TradeStagy)          // pair_tf:[stagyName]TradeStagy 所有的订阅策略
	InfoPairTFStags = make(map[string]map[string]*TradeStagy)          // pair_tf:[stagyName]TradeStagy 所有的辅助订阅策略

	BatchJobs   = map[string]map[string]*StagyJob{} // tf_account_stagy: pair: job 每个bar周期更新
	BatchInfos  = map[string]map[string]*StagyJob{} // tf_account_stagy: pair: job 每个info bar周期更新
	TFEnterMS   = map[string]int64{}                // tf: timeMS 执行入场的时间戳
	TFInfoMS    = map[string]int64{}                // tf: timeMS 执行Info的时间戳
	LastBatchMS = map[string]int64{}                // tf: timeMS 仅用于回测
)
