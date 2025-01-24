package strat

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/orm/ormo"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"math"
	"slices"
	"sort"
	"strings"
)

type FuncMakeStrat = func(pol *config.RunPolicyConfig) *TradeStrat

var (
	StratMake = make(map[string]FuncMakeStrat) // 已加载的策略缓存
)

func New(pol *config.RunPolicyConfig) *TradeStrat {
	polID := pol.ID()
	makeFn, ok := StratMake[pol.Name]
	var stgy *TradeStrat
	if ok {
		stgy = makeFn(pol)
	} else {
		panic("strategy not found: " + pol.Name)
		// stgy = loadNative(pol.Name)
	}
	stgy.Name = polID
	if stgy.MinTfScore == 0 {
		stgy.MinTfScore = 0.75
	}
	stgy.Policy = pol
	if pol.StakeRate > 0 {
		stgy.StakeRate = pol.StakeRate
	}
	if pol.OrderBarMax > 0 {
		stgy.OdBarMax = pol.OrderBarMax
	}
	return stgy
}

func Get(pair, stratID string) *TradeStrat {
	data, _ := PairStrats[pair]
	if len(data) == 0 {
		return nil
	}
	res, _ := data[stratID]
	return res
}

func GetStratPerf(pair, strat string) *config.StratPerfConfig {
	stg := Get(pair, strat)
	if stg == nil {
		return nil
	}
	if stg.Policy.StratPerf != nil {
		return stg.Policy.StratPerf
	}
	return config.Data.StratPerf
}

//func loadNative(stratName string) *TradeStrat {
//	filePath := path.Join(config.GetStratDir(), stratName+".o")
//	_, err := os.Stat(filePath)
//	nameVar := zap.String("name", stratName)
//	if err != nil {
//		log.Error("strategy not found", zap.String("path", filePath), zap.Error(err))
//		return nil
//	}
//	linker, err := goloader.ReadObj(filePath, stratName)
//	if err != nil {
//		log.Error("strategy load fail, package is `main`?", nameVar, zap.Error(err))
//		return nil
//	}
//	symPtr := make(map[string]uintptr)
//	err = goloader.RegSymbol(symPtr)
//	if err != nil {
//		log.Error("strategy read symbol fail", nameVar, zap.Error(err))
//		return nil
//	}
//	regLoaderTypes(symPtr)
//	module, err := goloader.Load(linker, symPtr)
//	if err != nil {
//		log.Error("strategy load module fail", nameVar, zap.Error(err))
//		return nil
//	}
//	keys := zap.String("keys", strings.Join(utils.KeysOfMap(module.Syms), ","))
//	prefix := stratName + "."
//	// 加载Main
//	mainPath := prefix + "Main"
//	mainPtr := GetModuleItem(module, mainPath)
//	if mainPtr == nil {
//		log.Error("module item not found", zap.String("p", mainPath), keys)
//		return nil
//	}
//	runFunc := *(*func() *TradeStrat)(mainPtr)
//	stagy := runFunc()
//	stagy.Name = stratName
//	// 这里不能卸载，卸载后结构体的嵌入函数无法调用
//	// module.Unload()
//	return stagy
//}
//
//func GetModuleItem(module *goloader.CodeModule, itemPath string) unsafe.Pointer {
//	main, ok := module.Syms[itemPath]
//	if !ok || main == 0 {
//		return nil
//	}
//	mainPtr := (uintptr)(unsafe.Pointer(&main))
//	return unsafe.Pointer(&mainPtr)
//}
//
//func regLoaderTypes(symPtr map[string]uintptr) {
//	goloader.RegTypes(symPtr, &ta.BarEnv{}, &ta.Series{}, &ta.CrossLog{}, &ta.XState{}, ta.Cross, ta.Sum, ta.SMA,
//		ta.EMA, ta.EMABy, ta.RMA, ta.RMABy, ta.TR, ta.ATR, ta.MACD, ta.MACDBy, ta.RSI, ta.Highest, ta.Lowest, ta.KDJ,
//		ta.KDJBy, ta.StdDev, ta.StdDevBy, ta.BBANDS, ta.TD, &ta.AdxState{}, ta.ADX, ta.ROC, ta.HeikinAshi)
//	stgy := &TradeStrat{}
//	goloader.RegTypes(symPtr, stgy, stgy.OnPairInfos, stgy.OnStartUp, stgy.OnBar, stgy.OnInfoBar, stgy.OnTrades,
//		stgy.OnCheckExit, stgy.GetDrawDownExitRate, stgy.PickTimeFrame, stgy.OnShutDown)
//	job := &StratJob{}
//	goloader.RegTypes(symPtr, job, job.OpenOrder)
//	goloader.RegTypes(symPtr, &PairSub{}, &EnterReq{}, &ExitReq{})
//}

func (q *ExitReq) Clone() *ExitReq {
	res := &ExitReq{
		Tag:        q.Tag,
		StratName:  q.StratName,
		EnterTag:   q.EnterTag,
		Dirt:       q.Dirt,
		OrderType:  q.OrderType,
		Limit:      q.Limit,
		ExitRate:   q.ExitRate,
		Amount:     q.Amount,
		OrderID:    q.OrderID,
		UnFillOnly: q.UnFillOnly,
		Force:      q.Force,
	}
	return res
}

func (s *StratJob) InitBar(curOrders []*ormo.InOutOrder) {
	s.CheckMS = btime.TimeMS()
	if s.IsWarmUp {
		s.LongOrders = nil
		s.ShortOrders = nil
	} else if s.OrderNum > 0 {
		s.LongOrders = nil
		s.ShortOrders = nil
		enteredNum := 0
		for _, od := range curOrders {
			if od.Symbol == s.Symbol.Symbol && od.Timeframe == s.TimeFrame && od.Strategy == s.Strat.Name {
				if od.Status >= ormo.InOutStatusPartEnter && od.Status <= ormo.InOutStatusPartExit {
					enteredNum += 1
				}
				if od.Short {
					s.ShortOrders = append(s.ShortOrders, od)
				} else {
					s.LongOrders = append(s.LongOrders, od)
				}
			}
		}
		s.OrderNum = len(s.LongOrders) + len(s.ShortOrders)
		s.EnteredNum = enteredNum
	}
	s.Entrys = nil
	s.Exits = nil
}

func (s *StratJob) SnapOrderStates() map[int64]*ormo.InOutSnap {
	var res = make(map[int64]*ormo.InOutSnap)
	orders := s.GetOrders(0)
	for _, od := range orders {
		res[od.ID] = od.TakeSnap()
	}
	return res
}

func (s *StratJob) CheckCustomExits(snap map[int64]*ormo.InOutSnap) ([]*ormo.InOutEdit, *errs.Error) {
	var res []*ormo.InOutEdit
	var skipSL, skipTP = 0, 0
	orders := s.GetOrders(0)
	for _, od := range orders {
		shot := od.TakeSnap()
		old, ok := snap[od.ID]
		if !od.CanClose() || od.Status < ormo.InOutStatusFullEnter {
			// Not fully entered yet, check if the limit price or trigger price has been updated
			// 尚未完全入场，检查限价或触发价格是否更新
			if !ok {
				continue
			}
			if old.EnterLimit != shot.EnterLimit {
				// Modify the entry price
				// 修改入场价格
				res = append(res, &ormo.InOutEdit{
					Order:  od,
					Action: ormo.OdActionLimitEnter,
				})
			}
		} else {
			// Those who have fully entered, check if they have exited
			// 已完全入场的，检查是否退出
			slEdit, tpEdit, err := s.checkOrderExit(od)
			if err != nil {
				return res, err
			}
			if slEdit != nil {
				if slEdit.Action == "" {
					skipSL += 1
				} else {
					res = append(res, slEdit)
				}
			} else if old != nil && (old.StopLoss != shot.StopLoss || old.StopLossLimit != shot.StopLossLimit) {
				// Updated stop loss elsewhere
				// 在其他地方更新了止损
				res = append(res, &ormo.InOutEdit{Order: od, Action: ormo.OdActionStopLoss})
			}
			if tpEdit != nil {
				if tpEdit.Action == "" {
					skipSL += 1
				} else {
					res = append(res, tpEdit)
				}
			} else if old != nil && (old.TakeProfit != shot.TakeProfit || old.TakeProfitLimit != shot.TakeProfitLimit) {
				// Updated profit taking in other places
				// 在其他地方更新了止盈
				res = append(res, &ormo.InOutEdit{Order: od, Action: ormo.OdActionTakeProfit})
			}
			if old.ExitLimit != shot.ExitLimit {
				// Modify the appearance price
				// 修改出场价格
				res = append(res, &ormo.InOutEdit{
					Order:  od,
					Action: ormo.OdActionLimitExit,
				})
			}
		}
	}
	if core.LiveMode && skipSL+skipTP > 0 {
		log.Warn(fmt.Sprintf("%s/%s triggers on exchange is disabled, stoploss: %v, takeprofit: %v",
			s.Strat.Name, s.Symbol.Symbol, skipSL, skipTP))
	}
	return res, nil
}

func (s *StratJob) checkOrderExit(od *ormo.InOutOrder) (*ormo.InOutEdit, *ormo.InOutEdit, *errs.Error) {
	sl := od.GetStopLoss().Clone()
	tp := od.GetTakeProfit().Clone()
	req, err := s.customExit(od)
	if err != nil || req != nil {
		return nil, nil, err
	}
	var slEdit, tpEdit *ormo.InOutEdit
	newSL := od.GetStopLoss()
	newTP := od.GetTakeProfit()
	// Check if the condition sheet needs to be modified
	// 检查是否需要修改条件单
	if sl != nil || newSL != nil {
		if sl == nil || newSL == nil || sl.Price != newSL.Price || sl.Limit != newSL.Limit {
			slEdit = &ormo.InOutEdit{Order: od, Action: ormo.OdActionStopLoss}
		}
	}
	if tp != nil || newTP != nil {
		if tp == nil || newTP == nil || tp.Price != newTP.Price || tp.Limit != newTP.Limit {
			tpEdit = &ormo.InOutEdit{Order: od, Action: ormo.OdActionTakeProfit}
		}
	}
	return slEdit, tpEdit, nil
}

/*
GetJobs 返回：pair_tf: [stratID]StratJob
*/
func GetJobs(account string) map[string]map[string]*StratJob {
	if !core.EnvReal {
		account = config.DefAcc
	}
	jobs, ok := AccJobs[account]
	if !ok {
		jobs = map[string]map[string]*StratJob{}
		AccJobs[account] = jobs
	}
	return jobs
}

func GetInfoJobs(account string) map[string]map[string]*StratJob {
	if !core.EnvReal {
		account = config.DefAcc
	}
	lockInfoJobs.Lock()
	jobs, ok := AccInfoJobs[account]
	if !ok {
		jobs = map[string]map[string]*StratJob{}
		AccInfoJobs[account] = jobs
	}
	lockInfoJobs.Unlock()
	return jobs
}

func CalcJobScores(pair, tf, stgy string) *errs.Error {
	var orders []*ormo.InOutOrder
	cfg := GetStratPerf(pair, stgy)
	if core.LiveMode {
		// Retrieve recent orders from the database
		// 从数据库查询最近订单
		sess, err := ormo.Conn(orm.DbTrades, false)
		if err != nil {
			return err
		}
		taskId := ormo.GetTaskID(config.DefAcc)
		orders, err = sess.GetOrders(ormo.GetOrdersArgs{
			TaskID:    taskId,
			Strategy:  stgy,
			TimeFrame: tf,
			Pairs:     []string{pair},
			Status:    2,
			Limit:     cfg.MaxOdNum,
		})
		if err != nil {
			return err
		}
	} else {
		// 从HistODs查询
		for _, od := range ormo.HistODs {
			if od.Symbol != pair || od.Timeframe != tf || od.Strategy != stgy {
				continue
			}
			orders = append(orders, od)
		}
		if len(orders) > cfg.MaxOdNum {
			orders = orders[len(orders)-cfg.MaxOdNum:]
		}
	}
	sta := core.GetPerfSta(stgy)
	sta.OdNum += 1
	if len(orders) < cfg.MinOdNum {
		return nil
	}
	totalPft := 0.0
	for _, od := range orders {
		totalPft += od.ProfitRate
	}
	var prefKey = core.KeyStratPairTf(stgy, pair, tf)
	perf, _ := core.JobPerfs[prefKey]
	if perf == nil {
		perf = &core.JobPerf{
			Num:       len(orders),
			TotProfit: totalPft,
			Score:     1,
		}
		core.JobPerfs[prefKey] = perf
	} else {
		perf.Num = len(orders)
	}
	// collect all trade jobs for this strategy & calculate stake rate
	// 收集此策略所有任务，计算开单倍率
	var prefs []*core.JobPerf
	for key, p := range core.JobPerfs {
		if strings.HasPrefix(key, stgy) {
			prefs = append(prefs, p)
		}
	}
	perf.Score = defaultCalcJobScore(cfg, sta, perf, prefs)
	if core.LiveMode {
		// Real disk mode, immediately save to data directory
		// 实盘模式，立刻保存到数据目录
		core.DumpPerfs(config.GetDataDir())
	}
	return nil
}

func defaultCalcJobScore(cfg *config.StratPerfConfig, sta *core.PerfSta, p *core.JobPerf, perfs []*core.JobPerf) float64 {
	if len(perfs) < cfg.MinJobNum {
		return 1
	}
	// 按Job总利润分组5档
	if sta.Splits == nil || sta.OdNum-sta.LastGpAt >= len(perfs) {
		// 按总收益率KMeans分组
		perfs = append(perfs, p)
		CalcJobPerfs(cfg, sta, perfs)
		return p.Score
	}
	// 聚类结果依然有效，查找最接近的pref，使用相同的Score
	var near *core.JobPerf
	var minDist = 0.0
	for _, pf := range perfs {
		dist := math.Abs(p.TotProfit - pf.TotProfit)
		if near == nil || dist < minDist {
			near = pf
			minDist = dist
		}
	}
	p.Score = near.Score
	return p.Score
}

func CalcJobPerfs(cfg *config.StratPerfConfig, p *core.PerfSta, perfs []*core.JobPerf) {
	sumProfit := 0.0
	var profits = make([]float64, 0, len(perfs))
	for _, pf := range perfs {
		profits = append(profits, pf.TotProfit)
		sumProfit += math.Abs(pf.TotProfit)
	}
	//Logarithmic processing of returns to make clustering more accurate
	//Map the average positive rate of return to x=9 in log2, ensuring that the logarithmic processing results in an appropriate distribution divergence
	// 对收益率对数处理，使聚类更准确
	// 将平均正收益率固定映射为log2的x=9，确保对数处理后能在合适的分布散度
	avgProfit := sumProfit / float64(len(profits))
	p.Delta = 9 / avgProfit
	for i, val := range profits {
		profits[i] = p.Log2(val)
	}
	res := utils.KMeansVals(profits, 5)
	groups := res.Clusters
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Center < groups[j].Center
	})
	var maxList = make([]float64, 0, len(groups))
	for _, gp := range groups {
		maxList = append(maxList, slices.Max(gp.Items))
	}
	// Calculate the upper and lower bounds of the rate of return for each group
	// 计算每组的收益率上下界
	p.Splits = &[4]float64{maxList[0], maxList[1], maxList[2], maxList[3]}
	p.LastGpAt = p.OdNum
	// Recalculate the weight of each job
	// 重新计算每个job的权重
	idxList := make([]int, 0, len(perfs))
	var totalAdd, goodNum, bestNum = 0.0, 0, 0
	for i, profit := range profits {
		pf := perfs[i]
		gid := p.FindGID(profit)
		if gid == 0 {
			pf.Score = core.PrefMinRate
			totalAdd += 1
		} else if gid == 1 {
			pf.Score = cfg.BadWeight
			totalAdd += 1 - cfg.BadWeight
		} else if gid == 2 {
			pf.Score = cfg.MidWeight
			totalAdd += 1 - cfg.MidWeight
		} else if gid == 3 {
			pf.Score = 1
			goodNum += 1
		} else if gid == 4 {
			pf.Score = 1
			bestNum += 1
		}
		idxList = append(idxList, gid)
	}
	if totalAdd > 0 {
		// Overlay the weight of losses onto profitable jobs
		// 将亏损的权重，叠加到盈利的job上
		totalWeight := float64(goodNum) + float64(bestNum)*2
		unitAdd := totalAdd / totalWeight
		for i, gid := range idxList {
			if gid < 3 {
				continue
			}
			pf := perfs[i]
			pf.Score += unitAdd * (float64(gid) - 2)
		}
	}
}

func AddOdSub(acc string, cb FnOdChange) {
	lockOdSub.Lock()
	defer lockOdSub.Unlock()
	subs, _ := accOdSubs[acc]
	accOdSubs[acc] = append(subs, cb)
}

func FireOdChange(acc string, od *ormo.InOutOrder, evt int) {
	subs, _ := accOdSubs[acc]
	subs2, _ := accOdSubs["*"]
	subs = append(subs, subs2...)
	for _, cb := range subs {
		cb(acc, od, evt)
	}
}

func AddStratGroup(group string, items map[string]FuncMakeStrat) {
	for k, v := range items {
		StratMake[group+":"+k] = v
	}
}

func (w Warms) Update(pair, tf string, num int) {
	if warms, ok := w[pair]; ok {
		if oldNum, ok := warms[tf]; ok {
			warms[tf] = max(oldNum, num)
		} else {
			warms[tf] = num
		}
	} else {
		w[pair] = map[string]int{tf: num}
	}
}

/*
JobForbidType 0 allow; 1 forbid; 2 forbid & occupy a slot
*/
func JobForbidType(pair, tf, stratName string) int {
	if jobs, ok := ForbidJobs[fmt.Sprintf("%s_%s", pair, tf)]; ok {
		hold, ok2 := jobs[stratName]
		if ok2 {
			if hold {
				return 2
			}
			return 1
		}
	}
	return 0
}

func GetJobKeys() map[string]map[string]bool {
	jobs := make(map[string]map[string]bool)
	for _, jobsMap := range AccJobs {
		for pairTF, stgMap := range jobsMap {
			idMap := make(map[string]bool)
			for polID := range stgMap {
				idMap[polID] = true
			}
			jobs[pairTF] = idMap
		}
		break
	}
	return jobs
}

func AddAccFailOpen(acc, tag string) {
	lockAccFailOpen.Lock()
	tagMap, ok1 := accFailOpens[acc]
	if !ok1 {
		tagMap = make(map[string]int)
		accFailOpens[acc] = tagMap
	}
	count, _ := tagMap[tag]
	tagMap[tag] = count + 1
	lockAccFailOpen.Unlock()
}

func DumpAccFailOpens() string {
	lockAccFailOpen.Lock()
	var b strings.Builder
	isFirst := true
	for k, v := range accFailOpens {
		if isFirst {
			isFirst = false
		} else {
			b.WriteString("\n")
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(utils.MapToStr(v, true, 0))
	}
	lockAccFailOpen.Unlock()
	return b.String()
}
