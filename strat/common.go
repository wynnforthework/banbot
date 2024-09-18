package strat

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"math"
	"slices"
	"sort"
	"strings"
)

type FuncMakeStagy = func(pol *config.RunPolicyConfig) *TradeStagy

var (
	StagyMake = make(map[string]FuncMakeStagy) // 已加载的策略缓存
)

func New(pol *config.RunPolicyConfig) *TradeStagy {
	polID := pol.ID()
	makeFn, ok := StagyMake[pol.Name]
	var stgy *TradeStagy
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
	return stgy
}

func Get(pair, strtgID string) *TradeStagy {
	data, _ := PairStags[pair]
	if len(data) == 0 {
		return nil
	}
	res, _ := data[strtgID]
	return res
}

func GetStrtgPerf(pair, strtg string) *config.StrtgPerfConfig {
	stg := Get(pair, strtg)
	if stg == nil {
		return nil
	}
	if stg.Policy.StrtgPerf != nil {
		return stg.Policy.StrtgPerf
	}
	return config.Data.StrtgPerf
}

//func loadNative(stagyName string) *TradeStagy {
//	filePath := path.Join(config.GetStagyDir(), stagyName+".o")
//	_, err := os.Stat(filePath)
//	nameVar := zap.String("name", stagyName)
//	if err != nil {
//		log.Error("strategy not found", zap.String("path", filePath), zap.Error(err))
//		return nil
//	}
//	linker, err := goloader.ReadObj(filePath, stagyName)
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
//	prefix := stagyName + "."
//	// 加载Main
//	mainPath := prefix + "Main"
//	mainPtr := GetModuleItem(module, mainPath)
//	if mainPtr == nil {
//		log.Error("module item not found", zap.String("p", mainPath), keys)
//		return nil
//	}
//	runFunc := *(*func() *TradeStagy)(mainPtr)
//	stagy := runFunc()
//	stagy.Name = stagyName
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
//	stgy := &TradeStagy{}
//	goloader.RegTypes(symPtr, stgy, stgy.OnPairInfos, stgy.OnStartUp, stgy.OnBar, stgy.OnInfoBar, stgy.OnTrades,
//		stgy.OnCheckExit, stgy.GetDrawDownExitRate, stgy.PickTimeFrame, stgy.OnShutDown)
//	job := &StagyJob{}
//	goloader.RegTypes(symPtr, job, job.OpenOrder)
//	goloader.RegTypes(symPtr, &PairSub{}, &EnterReq{}, &ExitReq{})
//}

func (q *ExitReq) Clone() *ExitReq {
	res := &ExitReq{
		Tag:        q.Tag,
		StgyName:   q.StgyName,
		EnterTag:   q.EnterTag,
		Dirt:       q.Dirt,
		OrderType:  q.OrderType,
		Limit:      q.Limit,
		ExitRate:   q.ExitRate,
		Amount:     q.Amount,
		OrderID:    q.OrderID,
		UnOpenOnly: q.UnOpenOnly,
		Force:      q.Force,
	}
	return res
}

func (s *StagyJob) InitBar(curOrders []*orm.InOutOrder) {
	s.CheckMS = btime.TimeMS()
	if s.IsWarmUp {
		s.LongOrders = nil
		s.ShortOrders = nil
	} else if s.OrderNum > 0 {
		s.LongOrders = nil
		s.ShortOrders = nil
		enteredNum := 0
		for _, od := range curOrders {
			if od.Strategy == s.Stagy.Name {
				if od.Status >= orm.InOutStatusFullEnter {
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

func (s *StagyJob) SnapOrderStates() map[int64]*orm.InOutSnap {
	var res = make(map[int64]*orm.InOutSnap)
	orders := s.GetOrders(0)
	for _, od := range orders {
		res[od.ID] = od.TakeSnap()
	}
	return res
}

func (s *StagyJob) CheckCustomExits(snap map[int64]*orm.InOutSnap) ([]*orm.InOutEdit, *errs.Error) {
	var res []*orm.InOutEdit
	var skipSL, skipTP = 0, 0
	orders := s.GetOrders(0)
	for _, od := range orders {
		shot := od.TakeSnap()
		old, ok := snap[od.ID]
		if !od.CanClose() || od.Status < orm.InOutStatusFullEnter {
			// 尚未完全入场，检查限价或触发价格是否更新
			if !ok {
				continue
			}
			if old.EnterLimit != shot.EnterLimit {
				// 修改入场价格
				res = append(res, &orm.InOutEdit{
					Order:  od,
					Action: orm.OdActionLimitEnter,
				})
			}
		} else {
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
				// 在其他地方更新了止损
				res = append(res, &orm.InOutEdit{Order: od, Action: orm.OdActionStopLoss})
			}
			if tpEdit != nil {
				if tpEdit.Action == "" {
					skipSL += 1
				} else {
					res = append(res, tpEdit)
				}
			} else if old != nil && (old.TakeProfit != shot.TakeProfit || old.TakeProfitLimit != shot.TakeProfitLimit) {
				// 在其他地方更新了止盈
				res = append(res, &orm.InOutEdit{Order: od, Action: orm.OdActionTakeProfit})
			}
			if old.ExitLimit != shot.ExitLimit {
				// 修改出场价格
				res = append(res, &orm.InOutEdit{
					Order:  od,
					Action: orm.OdActionLimitExit,
				})
			}
		}
	}
	if core.LiveMode && skipSL+skipTP > 0 {
		log.Warn(fmt.Sprintf("%s/%s triggers on exchange is disabled, stoploss: %v, takeprofit: %v",
			s.Stagy.Name, s.Symbol.Symbol, skipSL, skipTP))
	}
	return res, nil
}

func (s *StagyJob) checkOrderExit(od *orm.InOutOrder) (*orm.InOutEdit, *orm.InOutEdit, *errs.Error) {
	slPrice := od.GetInfoFloat64(orm.OdInfoStopLoss)
	tpPrice := od.GetInfoFloat64(orm.OdInfoTakeProfit)
	slLimit := od.GetInfoFloat64(orm.OdInfoStopLossLimit)
	tpLimit := od.GetInfoFloat64(orm.OdInfoTakeProfitLimit)
	req, err := s.customExit(od)
	if err != nil {
		return nil, nil, err
	}
	var slEdit, tpEdit *orm.InOutEdit
	if req == nil {
		// 检查是否需要修改条件单
		newSLPrice := s.LongSLPrice
		newTPPrice := s.LongTPPrice
		if od.Short {
			newSLPrice = s.ShortSLPrice
			newTPPrice = s.ShortTPPrice
		}
		if newSLPrice == 0 {
			newSLPrice = od.GetInfoFloat64(orm.OdInfoStopLoss)
		}
		if newTPPrice == 0 {
			newTPPrice = od.GetInfoFloat64(orm.OdInfoTakeProfit)
		}
		newSLLimit := od.GetInfoFloat64(orm.OdInfoStopLossLimit)
		newTPLimit := od.GetInfoFloat64(orm.OdInfoTakeProfitLimit)
		if newSLPrice != slPrice || slLimit != newSLLimit {
			if s.ExgStopLoss {
				slEdit = &orm.InOutEdit{Order: od, Action: orm.OdActionStopLoss}
				od.SetInfo(orm.OdInfoStopLoss, newSLPrice)
				od.SetInfo(orm.OdInfoStopLossLimit, newSLLimit)
			} else {
				slEdit = &orm.InOutEdit{}
				od.SetInfo(orm.OdInfoStopLoss, nil)
				od.SetInfo(orm.OdInfoStopLossLimit, nil)
			}
		}
		if newTPPrice != tpPrice || tpLimit != newTPLimit {
			if s.ExgTakeProfit {
				tpEdit = &orm.InOutEdit{Order: od, Action: orm.OdActionTakeProfit}
				od.SetInfo(orm.OdInfoTakeProfit, newTPPrice)
				od.SetInfo(orm.OdInfoTakeProfitLimit, newTPLimit)
			} else {
				tpEdit = &orm.InOutEdit{}
				od.SetInfo(orm.OdInfoTakeProfit, nil)
				od.SetInfo(orm.OdInfoTakeProfitLimit, nil)
			}
		}
	}
	return slEdit, tpEdit, nil
}

/*
GetJobs 返回：pair_tf: [stagyName]StagyJob
*/
func GetJobs(account string) map[string]map[string]*StagyJob {
	if !core.EnvReal {
		account = config.DefAcc
	}
	jobs, ok := AccJobs[account]
	if !ok {
		jobs = map[string]map[string]*StagyJob{}
		AccJobs[account] = jobs
	}
	return jobs
}

func GetInfoJobs(account string) map[string]map[string]*StagyJob {
	if !core.EnvReal {
		account = config.DefAcc
	}
	lockInfoJobs.Lock()
	jobs, ok := AccInfoJobs[account]
	if !ok {
		jobs = map[string]map[string]*StagyJob{}
		AccInfoJobs[account] = jobs
	}
	lockInfoJobs.Unlock()
	return jobs
}

func CalcJobScores(pair, tf, stagy string) *errs.Error {
	var orders []*orm.InOutOrder
	cfg := GetStrtgPerf(pair, stagy)
	if core.EnvReal {
		// 从数据库查询最近订单
		sess, conn, err := orm.Conn(nil)
		if err != nil {
			return err
		}
		defer conn.Release()
		taskId := orm.GetTaskID(config.DefAcc)
		orders, err = sess.GetOrders(orm.GetOrdersArgs{
			TaskID:    taskId,
			Strategy:  stagy,
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
		for _, od := range orm.HistODs {
			if od.Symbol != pair || od.Timeframe != tf || od.Strategy != stagy {
				continue
			}
			orders = append(orders, od)
		}
		if len(orders) > cfg.MaxOdNum {
			orders = orders[len(orders)-cfg.MaxOdNum:]
		}
	}
	sta := core.GetPerfSta(stagy)
	sta.OdNum += 1
	if len(orders) < cfg.MinOdNum {
		return nil
	}
	totalPft := 0.0
	for _, od := range orders {
		totalPft += od.ProfitRate
	}
	var prefKey = core.KeyStagyPairTf(stagy, pair, tf)
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
	var prefs []*core.JobPerf
	for key, p := range core.JobPerfs {
		if strings.HasPrefix(key, stagy) {
			prefs = append(prefs, p)
		}
	}
	// 计算开单倍率
	perf.Score = defaultCalcJobScore(cfg, stagy, perf, prefs)
	if core.LiveMode {
		// 实盘模式，立刻保存到数据目录
		core.DumpPerfs(config.GetDataDir())
	}
	return nil
}

func defaultCalcJobScore(cfg *config.StrtgPerfConfig, stagy string, p *core.JobPerf, perfs []*core.JobPerf) float64 {
	if len(perfs) < cfg.MinJobNum {
		return 1
	}
	// 按Job总利润分组5档
	sta := core.GetPerfSta(stagy)
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

func CalcJobPerfs(cfg *config.StrtgPerfConfig, p *core.PerfSta, perfs []*core.JobPerf) {
	sumProfit := 0.0
	var profits = make([]float64, 0, len(perfs))
	for _, pf := range perfs {
		profits = append(profits, pf.TotProfit)
		sumProfit += math.Abs(pf.TotProfit)
	}
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
	// 计算每组的收益率上下界
	p.Splits = &[4]float64{maxList[0], maxList[1], maxList[2], maxList[3]}
	p.LastGpAt = p.OdNum
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

func FireOdChange(acc string, od *orm.InOutOrder, evt int) {
	subs, _ := accOdSubs[acc]
	subs2, _ := accOdSubs["*"]
	subs = append(subs, subs2...)
	for _, cb := range subs {
		cb(acc, od, evt)
	}
}
