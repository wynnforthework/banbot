package strategy

import (
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	ta "github.com/banbox/banta"
	"go.uber.org/zap"
	"strings"
)

/*
LoadStagyJobs 加载策略和交易对

更新以下全局变量：
core.TFSecs
core.StgPairTfs
core.BookPairs
strategy.Versions
strategy.Envs
strategy.PairTFStags
strategy.AccJobs
strategy.AccInfoJobs

	返回对应关系：[(pair, timeframe, 预热数量, 策略列表), ...]
*/
func LoadStagyJobs(pairs []string, tfScores map[string]map[string]float64) (map[string]map[string]int, *errs.Error) {
	if len(pairs) == 0 || len(tfScores) == 0 {
		return nil, errs.NewMsg(errs.CodeParamRequired, "`pairs` and `tfScores` are required for LoadStagyJobs")
	}
	var exsList []*orm.ExSymbol
	for _, pair := range pairs {
		exs, err := orm.GetExSymbolCur(pair)
		if err != nil {
			return nil, err
		}
		exsList = append(exsList, exs)
	}
	// 将涉及的全局变量置为空，下面会更新
	core.TFSecs = make(map[string]int)
	core.BookPairs = make(map[string]bool)
	PairTFStags = make(map[string]map[string]*TradeStagy)
	for account := range AccInfoJobs {
		AccInfoJobs[account] = make(map[string]map[string]*StagyJob)
	}
	pairTfWarms := make(map[string]map[string]int)
	logWarm := func(pair, tf string, num int) {
		if warms, ok := pairTfWarms[pair]; ok {
			if oldNum, ok := warms[tf]; ok {
				warms[tf] = max(oldNum, num)
			} else {
				warms[tf] = num
			}
		} else {
			pairTfWarms[pair] = map[string]int{tf: num}
		}
	}
	for _, pol := range config.RunPolicy {
		stagy := Get(pol.Name)
		if stagy == nil {
			return pairTfWarms, errs.NewMsg(core.ErrRunTime, "strategy %s load fail", pol.Name)
		}
		Versions[stagy.Name] = stagy.Version
		stagyMaxNum := pol.MaxPair
		if stagyMaxNum == 0 {
			stagyMaxNum = 999
		}
		holdNum := 0
		newPairMap := make(map[string]string)
		for _, exs := range exsList {
			if holdNum > stagyMaxNum {
				break
			}
			tf := pickTimeFrame(stagy, exs, tfScores)
			if tf == "" {
				continue
			}
			holdNum += 1
			if _, ok := core.TFSecs[tf]; !ok {
				core.TFSecs[tf] = utils.TFToSecs(tf)
			}
			newPairMap[exs.Symbol] = tf
			envKey := strings.Join([]string{exs.Symbol, tf}, "_")
			if stagy.WatchBook {
				core.BookPairs[exs.Symbol] = true
			}
			items, ok := PairTFStags[envKey]
			if !ok {
				items = make(map[string]*TradeStagy)
				PairTFStags[envKey] = items
			}
			items[pol.Name] = stagy
			// 初始化BarEnv
			env := initBarEnv(exs, tf)
			// 记录需要预热的数据；记录订阅信息
			logWarm(exs.Symbol, tf, stagy.WarmupNum)
			for account := range config.Accounts {
				ensureStagyJob(stagy, account, tf, envKey, exs, env, logWarm)
			}
		}
		core.StgPairTfs[pol.Name] = newPairMap
	}
	initStagyJobs()
	// 确保所有pair、tf都在返回的中有记录，防止被数据订阅端移除
	for _, pairMap := range core.StgPairTfs {
		for pair, tf := range pairMap {
			tfMap, ok := pairTfWarms[pair]
			if !ok {
				tfMap = make(map[string]int)
				pairTfWarms[pair] = tfMap
			}
			if _, ok := tfMap[tf]; !ok {
				tfMap[tf] = 0
			}
		}
	}
	// 从Envs, AccJobs中删除无用的项
	for envKey := range Envs {
		if _, ok := PairTFStags[envKey]; !ok {
			delete(Envs, envKey)
		}
	}
	for _, jobs := range AccJobs {
		for envKey := range jobs {
			if _, ok := PairTFStags[envKey]; !ok {
				delete(AccJobs, envKey)
			}
		}
	}
	return pairTfWarms, nil
}

func pickTimeFrame(stagy *TradeStagy, exs *orm.ExSymbol, tfScores map[string]map[string]float64) string {
	scores, ok := tfScores[exs.Symbol]
	var tf string
	if ok {
		tf = stagy.pickTimeFrame(exs.Symbol, scores)
	}
	if tf == "" {
		scoreStrs := make([]string, 0, len(scores))
		for tf_, score := range scores {
			scoreStrs = append(scoreStrs, fmt.Sprintf("%v: %.3f", tf_, score))
		}
		log.Warn("filter pair by tfScore", zap.String("pair", exs.Symbol),
			zap.String("stagy", stagy.Name), zap.String("scores", strings.Join(scoreStrs, ", ")))
	}
	return tf
}

func initBarEnv(exs *orm.ExSymbol, tf string) *ta.BarEnv {
	envKey := strings.Join([]string{exs.Symbol, tf}, "_")
	env, ok := Envs[envKey]
	if !ok {
		tfMSecs := int64(utils.TFToSecs(tf) * 1000)
		env = &ta.BarEnv{
			Exchange:   core.ExgName,
			MarketType: core.Market,
			Symbol:     exs.Symbol,
			TimeFrame:  tf,
			TFMSecs:    tfMSecs,
			MaxCache:   core.NumTaCache,
			Data:       map[string]interface{}{"sid": exs.ID},
		}
		Envs[envKey] = env
	}
	return env
}

func ensureStagyJob(stagy *TradeStagy, account, tf, envKey string, exs *orm.ExSymbol, env *ta.BarEnv,
	logWarm func(pair, tf string, num int)) {
	jobs := GetJobs(account)
	envJobs, ok := jobs[envKey]
	if !ok {
		envJobs = make(map[string]*StagyJob)
		jobs[envKey] = envJobs
	}
	job, ok := envJobs[stagy.Name]
	if !ok {
		job = &StagyJob{
			Stagy:         stagy,
			Env:           env,
			Symbol:        exs,
			TimeFrame:     tf,
			Account:       account,
			TPMaxs:        make(map[int64]float64),
			OpenLong:      true,
			OpenShort:     true,
			CloseLong:     true,
			CloseShort:    true,
			ExgStopLoss:   true,
			ExgTakeProfit: true,
		}
		if stagy.OnStartUp != nil {
			stagy.OnStartUp(job)
		}
		envJobs[stagy.Name] = job
	}
	// 加载订阅其他标的信息
	if stagy.OnPairInfos != nil {
		infoJobs := GetInfoJobs(account)
		for _, s := range stagy.OnPairInfos(job) {
			logWarm(s.Pair, s.TimeFrame, s.WarmupNum)
			jobKey := strings.Join([]string{s.Pair, s.TimeFrame}, "_")
			items, ok := infoJobs[jobKey]
			if !ok {
				items = make(map[string]*StagyJob)
				infoJobs[jobKey] = items
			}
			items[stagy.Name] = job
		}
	}
}

func initStagyJobs() {
	// 更新job的EnterNum
	for account := range config.Accounts {
		openOds, lock := orm.GetOpenODs(account)
		var enterNums = make(map[string]int)
		lock.Lock()
		for _, od := range openOds {
			key := fmt.Sprintf("%s_%s_%s", od.Symbol, od.Timeframe, od.Strategy)
			num, ok := enterNums[key]
			if ok {
				enterNums[key] = num + 1
			} else {
				enterNums[key] = 1
			}
		}
		lock.Unlock()
		accJobs := GetJobs(account)
		for _, jobs := range accJobs {
			for _, job := range jobs {
				key := fmt.Sprintf("%s_%s_%s", job.Symbol.Symbol, job.TimeFrame, job.Stagy.Name)
				num, _ := enterNums[key]
				job.EnterNum = num
			}
		}
	}
}
