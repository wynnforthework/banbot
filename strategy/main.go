package strategy

import (
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
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
strategy.PairStags
strategy.AccJobs
strategy.AccInfoJobs

	返回对应关系：[(pair, timeframe, 预热数量, 策略列表), ...]
*/
func LoadStagyJobs(pairs []string, tfScores map[string]map[string]float64) (map[string]map[string]int, *errs.Error) {
	if len(pairs) == 0 || len(tfScores) == 0 {
		return nil, errs.NewMsg(errs.CodeParamRequired, "`pairs` and `tfScores` are required for LoadStagyJobs")
	}
	// 将涉及的全局变量置为空，下面会更新
	core.TFSecs = make(map[string]int)
	core.BookPairs = make(map[string]bool)
	core.StgPairTfs = make(map[string]map[string]string)
	PairStags = make(map[string]map[string]*TradeStagy)
	lockInfoJobs.Lock()
	for account := range AccInfoJobs {
		AccInfoJobs[account] = make(map[string]map[string]*StagyJob)
	}
	lockInfoJobs.Unlock()
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
	var envKeys = make(map[string]bool)
	stagyAdds := getStagyOpenPairs()
	for _, pol := range config.RunPolicy {
		stagy := New(pol)
		polID := pol.ID()
		if stagy == nil {
			return pairTfWarms, errs.NewMsg(core.ErrRunTime, "strategy %s load fail", polID)
		}
		Versions[stagy.Name] = stagy.Version
		stagyMaxNum := pol.MaxPair
		if stagyMaxNum == 0 {
			stagyMaxNum = 999
		}
		holdNum := 0
		newPairMap := make(map[string]string)
		failTfScores := make(map[string]map[string]float64)
		adds, _ := stagyAdds[polID]
		var curPairs, err = getPolicyPairs(pol, pairs, adds)
		if err != nil {
			return nil, err
		}
		var exsList []*orm.ExSymbol
		for _, pair := range curPairs {
			exs, err := orm.GetExSymbolCur(pair)
			if err != nil {
				return nil, err
			}
			exsList = append(exsList, exs)
		}
		dirt := pol.OdDirt()
		for _, exs := range exsList {
			if holdNum >= stagyMaxNum {
				break
			}
			curStagy := stagy
			scores, _ := tfScores[exs.Symbol]
			tf := curStagy.pickTimeFrame(exs.Symbol, scores)
			if tf == "" {
				failTfScores[exs.Symbol] = scores
				continue
			}
			items, ok := PairStags[exs.Symbol]
			if !ok {
				items = make(map[string]*TradeStagy)
				PairStags[exs.Symbol] = items
			}
			if _, ok = items[polID]; ok {
				// 当前pair+strtgID已有任务，跳过
				continue
			}
			// 检查有当前标的专有参数，重新初始化策略
			if curPol, isDiff := pol.PairDup(exs.Symbol); isDiff {
				curStagy = New(curPol)
			}
			items[polID] = curStagy
			holdNum += 1
			if _, ok = core.TFSecs[tf]; !ok {
				core.TFSecs[tf] = utils.TFToSecs(tf)
			}
			newPairMap[exs.Symbol] = tf
			envKey := strings.Join([]string{exs.Symbol, tf}, "_")
			if curStagy.WatchBook {
				core.BookPairs[exs.Symbol] = true
			}
			envKeys[envKey] = true
			// 初始化BarEnv
			env := initBarEnv(exs, tf)
			// 记录需要预热的数据；记录订阅信息
			logWarm(exs.Symbol, tf, curStagy.WarmupNum)
			for account := range config.Accounts {
				ensureStagyJob(curStagy, account, tf, envKey, exs, env, dirt, envKeys, logWarm)
			}
		}
		core.StgPairTfs[polID] = newPairMap
		printFailTfScores(polID, failTfScores)
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
		if _, ok := envKeys[envKey]; !ok {
			delete(Envs, envKey)
		}
	}
	for _, jobs := range AccJobs {
		for envKey := range jobs {
			if _, ok := envKeys[envKey]; !ok {
				delete(jobs, envKey)
			}
		}
	}
	return pairTfWarms, nil
}

func printFailTfScores(stagyName string, pairTfScores map[string]map[string]float64) {
	if len(pairTfScores) == 0 {
		return
	}
	lines := make([]string, 0, len(pairTfScores))
	for pair, tfScores := range pairTfScores {
		if len(tfScores) == 0 {
			lines = append(lines, fmt.Sprintf("%v: ", pair))
			continue
		}
		scoreStrs := make([]string, 0, len(pairTfScores))
		for tf_, score := range tfScores {
			scoreStrs = append(scoreStrs, fmt.Sprintf("%v: %.3f", tf_, score))
		}
		lines = append(lines, fmt.Sprintf("%v: %v", pair, strings.Join(scoreStrs, ", ")))
	}
	log.Info(fmt.Sprintf("%v filter pairs by tfScore: \n%v", stagyName, strings.Join(lines, "\n")))
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

func ensureStagyJob(stagy *TradeStagy, account, tf, envKey string, exs *orm.ExSymbol, env *ta.BarEnv, dirt int,
	envKeys map[string]bool, logWarm func(pair, tf string, num int)) {
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
			OpenLong:      dirt != core.OdDirtShort,
			OpenShort:     dirt != core.OdDirtLong,
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
			pair := s.Pair
			if pair == "_cur_" {
				pair = exs.Symbol
				initBarEnv(exs, s.TimeFrame)
			} else {
				curExs, err := orm.GetExSymbolCur(pair)
				if err != nil {
					log.Info("skip invalid pair", zap.String("pair", pair))
					continue
				}
				initBarEnv(curExs, s.TimeFrame)
			}
			logWarm(pair, s.TimeFrame, s.WarmupNum)
			jobKey := strings.Join([]string{pair, s.TimeFrame}, "_")
			items, ok := infoJobs[jobKey]
			if !ok {
				items = make(map[string]*StagyJob)
				infoJobs[jobKey] = items
			}
			items[stagy.Name] = job
			envKeys[jobKey] = true
		}
	}
}

func initStagyJobs() {
	// 更新job的EnterNum
	for account := range config.Accounts {
		openOds, lock := orm.GetOpenODs(account)
		var orderNums = make(map[string]int)
		var enteredNums = make(map[string]int)
		lock.Lock()
		for _, od := range openOds {
			key := fmt.Sprintf("%s_%s_%s", od.Symbol, od.Timeframe, od.Strategy)
			odNum, _ := orderNums[key]
			orderNums[key] = odNum + 1
			entNum, _ := enteredNums[key]
			enteredNums[key] = entNum + 1
		}
		lock.Unlock()
		accJobs := GetJobs(account)
		for _, jobs := range accJobs {
			for _, job := range jobs {
				key := fmt.Sprintf("%s_%s_%s", job.Symbol.Symbol, job.TimeFrame, job.Stagy.Name)
				odNum, _ := orderNums[key]
				job.OrderNum = odNum
				entNum, _ := enteredNums[key]
				job.EnteredNum = entNum
			}
		}
	}
}

var polFilters = make(map[string][]goods.IFilter)

func getPolicyPairs(pol *config.RunPolicyConfig, pairs []string, adds []string) ([]string, *errs.Error) {
	if len(pairs) == 0 {
		return pairs, nil
	}
	// 根据pol.Pairs确定交易的标的
	if len(pol.Pairs) > 0 {
		allAllows := make(map[string]bool)
		for _, p := range pairs {
			allAllows[p] = true
		}
		res := make([]string, 0, len(pol.Pairs))
		for _, p := range pol.Pairs {
			if _, has := allAllows[p]; has {
				res = append(res, p)
			}
		}
		return res, nil
	}
	if len(pol.Filters) == 0 {
		return pairs, nil
	}
	// 根据filters过滤筛选
	polID := pol.ID()
	filters, ok := polFilters[polID]
	var err *errs.Error
	if !ok {
		filters, err = goods.GetPairFilters(pol.Filters, false)
		if err != nil {
			return nil, err
		}
		polFilters[polID] = filters
	}
	var tickersMap map[string]*banexg.Ticker
	if core.LiveMode {
		for _, flt := range filters {
			if flt.IsNeedTickers() {
				tickersMap, err = exg.GetTickers()
				if err != nil {
					return nil, err
				}
				break
			}
		}
	}
	for _, flt := range filters {
		pairs, err = flt.Filter(pairs, tickersMap)
		if err != nil {
			return nil, err
		}
	}
	if len(adds) > 0 {
		err = orm.EnsureCurSymbols(adds)
		if err != nil {
			return nil, err
		}
		pairs = utils.UnionArr(adds, pairs)
		log.Info("add pairs while refresh", zap.Strings("add", adds))
	}
	return pairs, nil
}

func getStagyOpenPairs() map[string][]string {
	var data = make(map[string]map[string]bool)
	for account := range config.Accounts {
		openOds, lock := orm.GetOpenODs(account)
		lock.Lock()
		for _, od := range openOds {
			items, ok := data[od.Strategy]
			if !ok {
				items = make(map[string]bool)
				data[od.Strategy] = items
			}
			items[od.Symbol] = true
		}
		lock.Unlock()
	}
	var res = make(map[string][]string)
	for name, item := range data {
		var list = make([]string, 0, len(item))
		for pair := range item {
			list = append(list, pair)
		}
		res[name] = list
	}
	return res
}
