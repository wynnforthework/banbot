package strategy

import (
	ta "github.com/banbox/banta"
)

var (
	Versions    = make(map[string]int)           // 策略版本号
	Envs        = make(map[string]*ta.BarEnv)    // pair_tf: BarEnv
	Jobs        = make(map[string][]*StagyJob)   // pair_tf: []StagyJob
	InfoJobs    = make(map[string][]*StagyJob)   // pair_tf: []StagyJob 额外订阅
	PairTFStags = make(map[string][]*TradeStagy) // 所有的订阅策略
)
