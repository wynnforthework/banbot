package biz

import (
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/banbox/banexg/utils"
	"github.com/banbox/banta"
	"go.uber.org/zap"
	"math"
	"slices"
	"strings"
)

var (
	accOdMgrs     = make(map[string]IOrderMgr)
	accLiveOdMgrs = make(map[string]*LiveOrderMgr)
)

type IOrderMgr interface {
	ProcessOrders(sess *orm.Queries, env *banta.BarEnv, enters []*strat.EnterReq,
		exits []*strat.ExitReq, edits []*orm.InOutEdit) ([]*orm.InOutOrder, []*orm.InOutOrder, *errs.Error)
	EnterOrder(sess *orm.Queries, env *banta.BarEnv, req *strat.EnterReq, doCheck bool) (*orm.InOutOrder, *errs.Error)
	ExitOpenOrders(sess *orm.Queries, pairs string, req *strat.ExitReq) ([]*orm.InOutOrder, *errs.Error)
	ExitOrder(sess *orm.Queries, od *orm.InOutOrder, req *strat.ExitReq) (*orm.InOutOrder, *errs.Error)
	UpdateByBar(allOpens []*orm.InOutOrder, bar *orm.InfoKline) *errs.Error
	OnEnvEnd(bar *banexg.PairTFKline, adj *orm.AdjInfo) *errs.Error
	CleanUp() *errs.Error
}

type IOrderMgrLive interface {
	IOrderMgr
	SyncExgOrders() ([]*orm.InOutOrder, []*orm.InOutOrder, []*orm.InOutOrder, *errs.Error)
	WatchMyTrades()
	TrialUnMatchesForever()
	ConsumeOrderQueue()
}

type FuncHandleIOrder = func(order *orm.InOutOrder) *errs.Error

type OrderMgr struct {
	callBack    func(order *orm.InOutOrder, isEnter bool)
	afterEnter  FuncHandleIOrder
	afterExit   FuncHandleIOrder
	Account     string
	BarMS       int64
	simulOpen   int // Simultaneously open number in the current bar
	simulOpenSt map[string]int
}

func GetOdMgr(account string) IOrderMgr {
	if !core.EnvReal {
		account = config.DefAcc
	}
	val, _ := accOdMgrs[account]
	return val
}

func GetLiveOdMgr(account string) *LiveOrderMgr {
	if !core.EnvReal {
		panic("call GetLiveOdMgr in FakeEnv is forbidden: " + core.RunEnv)
	}
	val, _ := accLiveOdMgrs[account]
	return val
}

func CleanUpOdMgr() *errs.Error {
	var err *errs.Error
	for account := range config.Accounts {
		var curErr *errs.Error
		if core.EnvReal {
			if mgr, ok := accLiveOdMgrs[account]; ok {
				curErr = mgr.CleanUp()
			}
		} else {
			if mgr, ok := accOdMgrs[account]; ok {
				curErr = mgr.CleanUp()
			}
		}
		if curErr != nil {
			if err != nil {
				log.Error("clean odMgr fail", zap.String("acc", account), zap.Error(curErr))
			} else {
				err = curErr
			}
		}
	}
	return err
}

func (o *OrderMgr) allowOrderEnter(env *banta.BarEnv, enters []*strat.EnterReq) []*strat.EnterReq {
	if _, ok := core.ForbidPairs[env.Symbol]; ok {
		return nil
	}
	if core.RunMode == core.RunModeOther {
		// Does not involve order mode, prohibit opening orders
		// 不涉及订单模式，禁止开单
		return nil
	}
	pairZapField := zap.String("pair", env.Symbol)
	stopUntil, _ := core.NoEnterUntil[o.Account]
	if btime.TimeMS() < stopUntil {
		if core.LiveMode {
			log.Warn("any enter forbid", pairZapField)
		}
		return nil
	}
	if core.LiveMode {
		// The real order is submitted to the exchange, and the inspection delay cannot exceed 80%
		// 实盘订单提交到交易所，检查延迟不能超过80%
		rate := float64(btime.TimeMS()-env.TimeStop) / float64(env.TimeStop-env.TimeStart)
		if rate > 0.8 {
			return nil
		}
	}
	if o.BarMS < env.TimeStart {
		o.BarMS = env.TimeStart
		o.simulOpen = 0
		o.simulOpenSt = make(map[string]int)
	}
	openOds, lock := orm.GetOpenODs(o.Account)
	lock.Lock()
	enters = checkOrderNum(enters, len(openOds), config.MaxOpenOrders, "max_open_orders")
	if len(enters) > 0 && config.MaxSimulOpen > 0 {
		enters = checkOrderNum(enters, o.simulOpen, config.MaxSimulOpen, "max_simul_open")
	}
	if len(enters) == 0 {
		lock.Unlock()
		return nil
	}
	// Check whether the maximum number of orders opened by the strategy is exceeded
	// 检查是否超出策略最大开单数量
	stratOdNum := make(map[string]int)
	for _, od := range openOds {
		num, _ := stratOdNum[od.Strategy]
		stratOdNum[od.Strategy] = num + 1
	}
	res := make([]*strat.EnterReq, 0, len(enters))
	for _, req := range enters {
		num, _ := stratOdNum[req.StgyName]
		simulNum, _ := o.simulOpenSt[req.StgyName]
		pol := strat.Get(env.Symbol, req.StgyName).Policy
		if pol != nil {
			if pol.MaxOpen > 0 && num >= pol.MaxOpen {
				continue
			}
			if pol.MaxSimulOpen > 0 && simulNum >= pol.MaxSimulOpen {
				continue
			}
		}
		stratOdNum[req.StgyName] = num + 1
		o.simulOpenSt[req.StgyName] = simulNum + 1
		o.simulOpen += 1
		res = append(res, req)
	}
	lock.Unlock()
	return res
}

func checkOrderNum(enters []*strat.EnterReq, oldNum, maxNum int, tag string) []*strat.EnterReq {
	cutNum := oldNum + len(enters) - maxNum
	if maxNum > 0 && cutNum > 0 {
		if maxNum > oldNum {
			enters = enters[:maxNum-oldNum]
			if core.LiveMode {
				log.Warn("cut enters by", zap.String("tag", tag),
					zap.Int("left", len(enters)), zap.Int("cut", cutNum))
			}
		} else {
			enters = nil
			if core.LiveMode {
				log.Warn("skip enters by", zap.String("tag", tag), zap.Int("cut", cutNum))
			}
		}
	}
	return enters
}

/*
ProcessOrders
Execute order entry and exit requests
Create pending orders, the returned orders are not actually entered or exited;
Backtest: the caller executes the entry/exit order according to the next bar and updates the status
Live trading: monitor the exchange to return the order status to update the entry and exit
执行订单入场出场请求
创建待执行订单，返回的订单实际并未入场或出场；
回测：调用方根据下一个bar执行入场/出场订单，更新状态
实盘：监听交易所返回订单状态更新入场出场
*/
func (o *OrderMgr) ProcessOrders(sess *orm.Queries, env *banta.BarEnv, enters []*strat.EnterReq,
	exits []*strat.ExitReq) ([]*orm.InOutOrder, []*orm.InOutOrder, *errs.Error) {
	var entOrders, extOrders []*orm.InOutOrder
	if len(enters) > 0 {
		enters = o.allowOrderEnter(env, enters)
		for _, ent := range enters {
			iorder, err := o.EnterOrder(sess, env, ent, false)
			if err != nil {
				return entOrders, extOrders, err
			}
			entOrders = append(entOrders, iorder)
		}
	}
	if len(exits) > 0 {
		for _, exit := range exits {
			iorders, err := o.ExitOpenOrders(sess, env.Symbol, exit)
			if err != nil {
				return entOrders, extOrders, err
			}
			extOrders = append(extOrders, iorders...)
		}
	}
	return entOrders, extOrders, nil
}

func (o *OrderMgr) EnterOrder(sess *orm.Queries, env *banta.BarEnv, req *strat.EnterReq, doCheck bool) (*orm.InOutOrder, *errs.Error) {
	isSpot := core.Market == banexg.MarketSpot
	if req.Short && isSpot {
		return nil, errs.NewMsg(core.ErrRunTime, "short oder is invalid for spot")
	}
	if doCheck {
		enters := o.allowOrderEnter(env, []*strat.EnterReq{req})
		if len(enters) == 0 {
			return nil, nil
		}
	}
	if req.Leverage == 0 && !isSpot {
		exchange := exg.Default
		exInfo := exchange.Info()
		if exInfo.FixedLvg {
			req.Leverage, _ = exchange.GetLeverage(env.Symbol, 0, o.Account)
		} else {
			req.Leverage = config.GetAccLeverage(o.Account)
		}
	}
	stgVer, _ := strat.Versions[req.StgyName]
	odSide := banexg.OdSideBuy
	if req.Short {
		odSide = banexg.OdSideSell
	}
	taskId := orm.GetTaskID(o.Account)
	od := &orm.InOutOrder{
		IOrder: &orm.IOrder{
			TaskID:    taskId,
			Symbol:    env.Symbol,
			Sid:       utils.GetMapVal(env.Data, "sid", int32(0)),
			Timeframe: env.TimeFrame,
			Short:     req.Short,
			Status:    orm.InOutStatusInit,
			EnterTag:  req.Tag,
			InitPrice: core.GetPrice(env.Symbol),
			Leverage:  req.Leverage,
			EnterAt:   btime.TimeMS(),
			Strategy:  req.StgyName,
			StgVer:    int32(stgVer),
		},
		Enter: &orm.ExOrder{
			TaskID:    taskId,
			Symbol:    env.Symbol,
			Enter:     true,
			OrderType: core.OrderTypeEnums[req.OrderType],
			Side:      odSide,
			Price:     req.Limit,
			Amount:    req.Amount,
			Status:    orm.OdStatusInit,
		},
		Info:       map[string]interface{}{},
		DirtyMain:  true,
		DirtyEnter: true,
	}
	if od.Enter.OrderType == "" {
		od.Enter.OrderType = config.OrderType
	}
	if req.Limit > 0 {
		od.InitPrice = req.Limit
		if req.StopBars == 0 {
			req.StopBars = config.StopEnterBars
		}
		if req.StopBars > 0 {
			stopAfter := btime.TimeMS() + int64(req.StopBars*utils.TFToSecs(od.Timeframe))*1000
			od.SetInfo(orm.OdInfoStopAfter, stopAfter)
		}
	}
	od.SetInfo(orm.OdInfoLegalCost, req.LegalCost)
	if req.StopLoss > 0 {
		od.SetStopLoss(&orm.ExitTrigger{
			Price: req.StopLoss,
			Limit: req.StopLossLimit,
			Rate:  req.StopLossRate,
			Tag:   req.StopLossTag,
		})
	}
	if req.TakeProfit > 0 {
		od.SetTakeProfit(&orm.ExitTrigger{
			Price: req.TakeProfit,
			Limit: req.TakeProfitLimit,
			Rate:  req.TakeProfitRate,
			Tag:   req.TakeProfitTag,
		})
	}
	err := od.Save(sess)
	if err != nil {
		return od, err
	}
	if o.afterEnter != nil {
		err = o.afterEnter(od)
	}
	return od, err
}

func (o *OrderMgr) ExitOpenOrders(sess *orm.Queries, pairs string, req *strat.ExitReq) ([]*orm.InOutOrder, *errs.Error) {
	// Filter matching orders 筛选匹配的订单
	var matches []*orm.InOutOrder
	openOds, lock := orm.GetOpenODs(o.Account)
	if req.OrderID > 0 {
		// Specify the exact order ID to exit 精确指定退出的订单ID
		lock.Lock()
		od, ok := openOds[req.OrderID]
		lock.Unlock()
		if !ok {
			return nil, errs.NewMsg(errs.CodeParamInvalid, "req orderId not found: %d", req.OrderID)
		}
		matches = append(matches, od)
	} else {
		parts := strings.Split(pairs, ",")
		pairMap := make(map[string]bool)
		for _, p := range parts {
			if p == "" {
				continue
			}
			pairMap[p] = true
		}
		dirtBoth := req.Dirt == core.OdDirtBoth
		isShort := req.Dirt == core.OdDirtShort
		lock.Lock()
		for _, od := range openOds {
			if req.StgyName != "" && od.Strategy != req.StgyName {
				continue
			}
			if len(pairMap) > 0 {
				if _, ok := pairMap[od.Symbol]; !ok {
					continue
				}
			}
			if !dirtBoth && isShort != od.Short {
				continue
			}
			if req.EnterTag != "" && od.EnterTag != req.EnterTag {
				continue
			}
			if od.ExitTag != "" || (od.Exit != nil && od.Exit.Amount > 0) {
				// Order Exited 订单已退出
				continue
			}
			if req.UnFillOnly && od.Enter.Filled >= od.Enter.Amount {
				continue
			}
			if req.FilledOnly && od.Enter.Filled < core.AmtDust {
				continue
			}
			matches = append(matches, od)
		}
		lock.Unlock()
	}
	if len(matches) == 0 {
		return nil, nil
	}
	var exitAmount float64
	useRate := req.ExitRate > 0 && req.ExitRate < 1
	if useRate || req.Amount <= 0 {
		// Calculate the amount to withdraw 计算要退出的数量
		allAmount := float64(0)
		for _, od := range matches {
			allAmount += od.Enter.Amount
			if od.Exit != nil {
				allAmount -= od.Exit.Amount
			}
		}
		exitAmount = allAmount
		if useRate {
			exitAmount = allAmount * req.ExitRate
		}
	} else {
		exitAmount = req.Amount
	}
	isTakeProfit := false
	if req.Limit > 0 && core.IsLimitOrder(req.OrderType) {
		symbol := matches[0].Symbol
		for _, od := range matches[1:] {
			if od.Symbol != symbol {
				return nil, errs.NewMsg(errs.CodeParamInvalid, "ExitReq.Limit invalid for multi pairs")
			}
		}
		price := core.GetPrice(symbol)
		if price > 0 && (req.Limit-price)*float64(req.Dirt) > 0 {
			isTakeProfit = true
		}
	}
	slices.SortFunc(matches, func(a, b *orm.InOutOrder) int {
		fillA := a.Enter.Filled * a.InitPrice
		fillB := b.Enter.Filled * b.InitPrice
		fillChg := int(math.Round((fillA - fillB) * 100))
		// For profit taking or filled only, descending order by filled amount.
		// 对于止盈或退出已入场的，优先按已入场金额降序
		if (isTakeProfit || req.FilledOnly) && fillChg != 0 {
			// 止盈单，优先按入场金额倒序
			return -fillChg
		}
		costA := a.Enter.Amount * a.InitPrice
		unfillA := costA - fillA
		costB := b.Enter.Amount * b.InitPrice
		unfillB := costB - fillB
		// First, in descending order by unsold amount. 首先按未成交金额倒序
		res := int(math.Round((unfillB - unfillA) * 100))
		if res != 0 {
			return res
		}
		// Secondly, in ascending order by deposit amount 其次按已入场金额升序
		if fillChg != 0 {
			return res
		}
		// Last entry time ascending 最后按入场时间升序
		return int((a.EnterAt - b.EnterAt) / 1000)
	})
	var result []*orm.InOutOrder
	var part *orm.InOutOrder
	var err *errs.Error
	for i, od := range matches {
		if !req.Force && !od.CanClose() {
			continue
		}
		dust := od.Enter.Amount * 0.01
		if exitAmount < dust {
			if isTakeProfit {
				// reset TakeProfit for remaining orders
				// 剩余订单重置TakeProfit
				for _, odr := range matches[i:] {
					odr.SetTakeProfit(nil)
					_, err = o.postOrderExit(sess, odr)
					if err != nil {
						return result, err
					}
				}
			}
			break
		}
		if req.FilledOnly && od.Enter.Filled < od.Enter.Amount {
			// Only exit the entered orders, the current order is partially entered and divided into sub-orders
			// 只退出已入场的订单，当前订单部分入场，切分成子订单
			cutAmt := min(exitAmount, od.Enter.Filled)
			part = od.CutPart(cutAmt, 0)
			err = od.Save(sess)
			if err != nil {
				return result, err
			}
			od = part
		}
		q := req.Clone()
		q.ExitRate = min(1, exitAmount/od.Enter.Amount)
		if isTakeProfit && od.Status >= orm.InOutStatusPartEnter {
			od.SetTakeProfit(&orm.ExitTrigger{
				Price: q.Limit,
				Rate:  q.ExitRate,
				Tag:   q.Tag,
			})
			part, err = o.postOrderExit(sess, od)
		} else {
			part, err = o.exitOrder(sess, od, q)
		}
		if err != nil {
			return result, err
		}
		if part != nil {
			exitAmount -= part.Enter.Amount * q.ExitRate
			result = append(result, part)
		}
	}
	return result, nil
}

func (o *OrderMgr) ExitOrder(sess *orm.Queries, od *orm.InOutOrder, req *strat.ExitReq) (*orm.InOutOrder, *errs.Error) {
	if od.ExitTag != "" || (od.Exit != nil && od.Exit.Amount > 0) {
		// Exit一旦有值，表示全部退出
		return nil, nil
	}
	if req.Dirt != 0 && (req.Dirt < 0) != od.Short {
		return nil, errs.NewMsg(errs.CodeParamInvalid, "`ExitReq.Dirt` mismatch with Order")
	}
	if req.Limit > 0 && core.IsLimitOrder(req.OrderType) {
		price := core.GetPrice(od.Symbol)
		if price > 0 && (req.Limit-price)*float64(req.Dirt) > 0 {
			// It is a valid limit order, set to take profit
			// 是有效的限价出场单，设置到止盈中
			od.SetTakeProfit(&orm.ExitTrigger{
				Price: req.Limit,
				Rate:  req.ExitRate,
				Tag:   req.Tag,
			})
			return o.postOrderExit(sess, od)
		}
	}
	return o.exitOrder(sess, od, req)
}

func (o *OrderMgr) exitOrder(sess *orm.Queries, od *orm.InOutOrder, req *strat.ExitReq) (*orm.InOutOrder, *errs.Error) {
	// It has been confirmed externally that it is not a limit price stop profit
	// 外部已确认不是限价止盈
	odType := core.OrderTypeEnums[req.OrderType]
	if odType == "" {
		odType = config.OrderType
	}
	if req.ExitRate < 0.99 && req.ExitRate > 0 {
		// The portion to be exited is less than 99%, so a small order is split out for exit.
		// 要退出的部分不足99%，分割出一个小订单，用于退出。
		part := o.CutOrder(od, req.ExitRate, 0)
		req.ExitRate = 1
		err := od.Save(sess)
		if err != nil {
			log.Error("save cutPart parent order fail", zap.String("key", od.Key()), zap.Error(err))
		}
		return o.exitOrder(sess, part, req)
	}
	od.SetExit(req.Tag, odType, 0)
	return o.postOrderExit(sess, od)
}

func (o *OrderMgr) postOrderExit(sess *orm.Queries, od *orm.InOutOrder) (*orm.InOutOrder, *errs.Error) {
	err := od.Save(sess)
	if err != nil {
		return od, err
	}
	if o.afterExit != nil {
		err = o.afterExit(od)
	}
	return od, err
}

/*
UpdateByBar
Use the price to update the profit of the order, etc. It may trigger a margin call
使用价格更新订单的利润等。可能会触发爆仓
*/
func (o *OrderMgr) UpdateByBar(allOpens []*orm.InOutOrder, bar *orm.InfoKline) *errs.Error {
	for _, od := range allOpens {
		if od.Symbol != bar.Symbol || od.Timeframe != bar.TimeFrame || od.Status >= orm.InOutStatusFullExit {
			continue
		}
		od.UpdateProfits(bar.Close)
	}
	return nil
}

func (o *OrderMgr) CutOrder(od *orm.InOutOrder, enterRate, exitRate float64) *orm.InOutOrder {
	part := od.CutPart(od.Enter.Amount*enterRate, od.Enter.Amount*exitRate)
	// Here the key of part is the same as the original one, so part is used as src_key
	// 这里part的key和原始的一样，所以part作为src_key
	tgtKey, srcKey := od.Key(), part.Key()
	base, quote, _, _ := core.SplitSymbol(od.Symbol)
	wallets := GetWallets(o.Account)
	wallets.CutPart(srcKey, tgtKey, base, 1-enterRate)
	wallets.CutPart(srcKey, tgtKey, quote, 1-enterRate)
	return part
}

/*
finishOrder
sess 可为nil
It will be saved internally to the database during the actual trading.
实盘时内部会保存到数据库。
*/
func (o *OrderMgr) finishOrder(od *orm.InOutOrder, sess *orm.Queries) *errs.Error {
	od.UpdateProfits(0)
	err := od.Save(sess)
	cfg := strat.GetStratPerf(od.Symbol, od.Strategy)
	if cfg != nil && cfg.Enable && o.Account == config.DefAcc {
		err2 := strat.CalcJobScores(od.Symbol, od.Timeframe, od.Strategy)
		if err2 != nil {
			log.Error("calc job performance fail", zap.Error(err2),
				zap.Strings("job", []string{od.Symbol, od.Timeframe, od.Strategy}))
		}
	}
	tipAmtLock.Lock()
	// Order opened successfully, prompt allowed
	delete(tipAmtZeros, od.Symbol) // 开单成功，允许提示
	tipAmtLock.Unlock()
	return err
}

func (o *OrderMgr) CleanUp() *errs.Error {
	return nil
}
