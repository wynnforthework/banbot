package strategy

import (
	ta "github.com/banbox/banta"
)

var (
	Versions    = make(map[string]int)                             // 策略版本号
	Envs        = make(map[string]*ta.BarEnv)                      // pair_tf: BarEnv
	AccJobs     = make(map[string]map[string]map[string]*StagyJob) // account: pair_tf: [stagyName]StagyJob
	AccInfoJobs = make(map[string]map[string]map[string]*StagyJob) // account: pair_tf: [stagyName]StagyJob 额外订阅
	PairTFStags = make(map[string]map[string]*TradeStagy)          // pair_tf:[stagyName]TradeStagy 所有的订阅策略
)
