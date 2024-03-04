package strategy

import (
	ta "github.com/banbox/banta"
)

var (
	Versions    = make(map[string]int)                    // 策略版本号
	Envs        = make(map[string]*ta.BarEnv)             // pair_tf: BarEnv
	AccJobs     = make(map[string]map[string][]*StagyJob) // account:pair_tf: []StagyJob
	AccInfoJobs = make(map[string]map[string][]*StagyJob) // account:pair_tf: []StagyJob 额外订阅
	PairTFStags = make(map[string][]*TradeStagy)          // 所有的订阅策略
)
