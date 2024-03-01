package biz

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/banbox/banta"
	"go.uber.org/zap"
	"math"
	"strconv"
	"strings"
	"time"
)

type FuncApplyMyTrade = func(od *orm.InOutOrder, subOd *orm.ExOrder, trade *banexg.MyTrade) *errs.Error
type FuncHandleMyTrade = func(trade *banexg.MyTrade) bool

type LiveOrderMgr struct {
	OrderMgr
	queue            chan *OdQItem
	doneKeys         map[string]bool // 已完成的订单：symbol+orderId
	volPrices        map[string]*VolPrice
	exgIdMap         map[string]*orm.InOutOrder
	doneTrades       map[string]bool            // 已处理的交易：symbol+tradeId
	isWatchMyTrade   bool                       // 是否正在监听账户交易流
	isTrialUnMatches bool                       // 是否正在监听未匹配交易
	isConsumeOrderQ  bool                       // 是否正在从订单队列消费
	unMatchTrades    map[string]*banexg.MyTrade // 从ws收到的暂无匹配的订单的交易
	applyMyTrade     FuncApplyMyTrade
	exitByMyTrade    FuncHandleMyTrade
	traceExgOrder    FuncHandleMyTrade
}

type OdQItem struct {
	Order  *orm.InOutOrder
	Action string
}

const (
	AmtDust = 1e-8
)

var (
	IsWatchAccConfig = false
	pairVolMap       = map[string]*PairValItem{}
)

type PairValItem struct {
	AvgVol   float64
	LastVol  float64
	ExpireMS int64
}

func InitLiveOrderMgr(callBack func(od *orm.InOutOrder, isEnter bool)) {
	res := &LiveOrderMgr{
		OrderMgr: OrderMgr{
			callBack: callBack,
		},
		queue:         make(chan *OdQItem, 1000),
		doneKeys:      map[string]bool{},
		volPrices:     map[string]*VolPrice{},
		exgIdMap:      map[string]*orm.InOutOrder{},
		doneTrades:    map[string]bool{},
		unMatchTrades: map[string]*banexg.MyTrade{},
	}
	res.afterEnter = makeAfterEnter(res)
	res.afterExit = makeAfterExit(res)
	if core.ExgName == "binance" {
		res.applyMyTrade = bnbApplyMyTrade(res)
		res.exitByMyTrade = bnbExitByMyTrade(res)
		res.traceExgOrder = bnbTraceExgOrder(res)
	} else {
		panic("unsupport exchange for LiveOrderMgr: " + core.ExgName)
	}
	OdMgrLive = res
	OdMgr = res
}

/*
SyncExgOrders
将交易所最新状态本地订单进行同步

	先通过fetch_account_positions抓取交易所所有币的仓位情况。
	如果本地没有未平仓订单：
	    如果交易所没有持仓：忽略
	    如果交易所有持仓：视为用户开的新订单，创建新订单跟踪
	如果本地有未平仓订单：
	     获取本地订单的最后时间作为起始时间，通过fetch_orders接口查询此后所有订单。
	     从交易所订单记录来确定未平仓订单的当前状态：已平仓、部分平仓、未平仓
	     对于冗余的仓位，视为用户开的新订单，创建新订单跟踪。
*/
func (o *LiveOrderMgr) SyncExgOrders() ([]*orm.InOutOrder, []*orm.InOutOrder, []*orm.InOutOrder, *errs.Error) {
	exchange := exg.Default
	sess, conn, err := orm.Conn(nil)
	if err != nil {
		return nil, nil, nil, err
	}
	// 从数据库加载订单
	orm.OpenODs = make(map[int64]*orm.InOutOrder)
	orders, err := sess.GetOrders(orm.GetOrdersArgs{
		TaskID: -1,
		Limit:  1000,
	})
	// 这里加载完订单就释放，防止长时间占用连接
	conn.Release()
	orm.HistODs = make([]*orm.InOutOrder, 0, len(orders))
	if err != nil {
		return nil, nil, nil, err
	}
	var sinceMS int64
	var openPairs = map[string]struct{}{}
	for _, od := range orders {
		if od.Status < orm.InOutStatusFullExit {
			tryOd := od.Enter
			if od.Exit != nil {
				tryOd = od.Exit
			}
			if tryOd.Enter && tryOd.OrderID == "" && tryOd.Status == orm.OdStatusInit {
				// 订单未提交到交易所，且是入场订单
				if isFarLimit(tryOd) {
					orm.AddTriggerOd(od)
				} else {
					err = od.LocalExit(core.ExitTagForceExit, 0, "重启取消未入场订单")
					if err != nil {
						log.Error("cancel nonEntry order fail", zap.String("key", od.Key()), zap.Error(err))
					}
					continue
				}
			}
			orm.OpenODs[od.ID] = od
			sinceMS = max(sinceMS, od.EnterAt)
			openPairs[od.Symbol] = struct{}{}
		} else {
			orm.HistODs = append(orm.HistODs, od)
		}
	}
	// 获取交易所仓位
	posList, err := exchange.FetchAccountPositions(nil, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	posMap := utils.ArrToMap(posList, func(v *banexg.Position) string {
		openPairs[v.Symbol] = struct{}{}
		return v.Symbol
	})
	var resOdList = make([]*orm.InOutOrder, 0, len(orm.OpenODs))
	for pair := range openPairs {
		curOds := make([]*orm.InOutOrder, 0, 2)
		for _, od := range orm.OpenODs {
			if od.Symbol == pair {
				curOds = append(curOds, od)
			}
		}
		curPos, _ := posMap[pair]
		if len(curPos) == 0 && len(curOds) == 0 {
			continue
		}
		var longPos, shortPos *banexg.Position
		for _, pos := range curPos {
			if pos.Side == banexg.PosSideLong {
				longPos = pos
			} else {
				shortPos = pos
			}
		}
		var prevTF string
		for _, od := range orm.HistODs {
			if od.Symbol == pair {
				prevTF = od.Timeframe
				break
			}
		}
		curOds, err = o.syncPairOrders(pair, prevTF, longPos, shortPos, sinceMS, curOds)
		if err != nil {
			return nil, nil, nil, err
		}
		resOdList = append(resOdList, curOds...)
	}
	var oldList = make([]*orm.InOutOrder, 0, 4)
	var newList = make([]*orm.InOutOrder, 0, 4)
	var delList = make([]*orm.InOutOrder, 0, 4)
	resMap := utils.ArrToMap(resOdList, func(od *orm.InOutOrder) int64 {
		return od.ID
	})
	for key, od := range orm.OpenODs {
		_, newHas := resMap[key]
		if !newHas {
			delList = append(delList, od)
		}
	}
	for key, od := range resMap {
		_, oldHas := orm.OpenODs[key]
		if oldHas {
			oldList = append(oldList, od...)
		} else {
			newList = append(newList, od...)
			orm.OpenODs[key] = od[0]
		}
	}
	if len(oldList) > 0 {
		log.Info(fmt.Sprintf("恢复%v个未平仓订单", len(oldList)))
	}
	if len(newList) > 0 {
		log.Info(fmt.Sprintf("新开始跟踪%v个用户下单", len(newList)))
	}
	return oldList, newList, delList, nil
}

/*
对指定币种，将交易所订单状态同步到本地。机器人刚启动时执行。
*/
func (o *LiveOrderMgr) syncPairOrders(pair, defTF string, longPos, shortPos *banexg.Position, sinceMs int64,
	openOds []*orm.InOutOrder) ([]*orm.InOutOrder, *errs.Error) {
	var exOrders []*banexg.Order
	var err *errs.Error
	if len(openOds) > 0 {
		// 本地有未平仓订单，从交易所获取订单记录，尝试恢复订单状态。
		exOrders, err = exg.Default.FetchOrders(pair, sinceMs, 0, nil)
		if err != nil {
			return openOds, err
		}
	}
	// 获取交易所订单后再获取连接，减少占用时长
	sess, conn, err := orm.Conn(nil)
	if err != nil {
		return openOds, err
	}
	defer conn.Release()
	if len(openOds) > 0 {
		for _, exod := range exOrders {
			if exod.Status != banexg.OdStatusClosed {
				// 跳过未完成订单
				continue
			}
			openOds, err = o.applyHisOrder(sess, openOds, exod, defTF)
			if err != nil {
				return openOds, err
			}
		}
		var longPosAmt, shortPosAmt float64
		if longPos != nil {
			longPosAmt = longPos.Contracts
		}
		if shortPos != nil {
			shortPosAmt = shortPos.Contracts
		}
		// 检查剩余的打开订单是否和仓位匹配，如不匹配强制关闭对应的订单
		for _, iod := range openOds {
			odAmt := iod.Enter.Filled
			if iod.Exit != nil {
				odAmt -= iod.Exit.Filled
			}
			if odAmt*iod.InitPrice < 1 {
				// TODO: 这里计算的quote价值，后续需要改为法币价值
				if iod.Status < orm.InOutStatusFullExit {
					msg := "订单没有入场仓位"
					err = iod.LocalExit(core.ExitTagFatalErr, iod.InitPrice, msg)
					if err != nil {
						return openOds, err
					}
				}
				openOds = utils.RemoveFromArr(openOds, iod, 1)
				continue
			}
			posAmt := longPosAmt
			if iod.Short {
				posAmt = shortPosAmt
			}
			posAmt -= odAmt
			if iod.Short {
				shortPosAmt = posAmt
			} else {
				longPosAmt = posAmt
			}
			if posAmt < odAmt*-0.01 {
				msg := fmt.Sprintf("订单在交易所没有对应仓位，交易所：%.5f", posAmt+odAmt)
				err = iod.LocalExit(core.ExitTagFatalErr, iod.InitPrice, msg)
				if err != nil {
					return openOds, err
				}
				openOds = utils.RemoveFromArr(openOds, iod, 1)
			}
		}
		if longPos != nil {
			longPos.Contracts = longPosAmt
		}
		if shortPos != nil {
			shortPos.Contracts = shortPosAmt
		}
	}
	if config.TakeOverStgy == "" {
		return openOds, nil
	}
	if longPos != nil && longPos.Contracts > AmtDust {
		longOd, err := o.createOdFromPos(longPos, defTF)
		if err != nil {
			return openOds, err
		}
		openOds = append(openOds, longOd)
		err = longOd.Save(sess)
		if err != nil {
			return openOds, err
		}
	}
	if shortPos != nil && shortPos.Contracts > AmtDust {
		shortOd, err := o.createOdFromPos(shortPos, defTF)
		if err != nil {
			return openOds, err
		}
		openOds = append(openOds, shortOd)
		err = shortOd.Save(sess)
		if err != nil {
			return openOds, err
		}
	}
	return openOds, nil
}

func getFeeNameCost(fee *banexg.Fee, pair, odType, side string, amount, price float64) (string, float64) {
	isMaker := false
	if fee != nil {
		if fee.Cost > 0 {
			return fee.Currency, fee.Cost
		}
		isMaker = fee.IsMaker
	} else {
		isMaker = odType != banexg.OdTypeMarket
	}
	fee, err := exg.Default.CalculateFee(pair, odType, side, amount, price, isMaker, nil)
	if err != nil {
		log.Error("calc fee fail getFeeNameCost", zap.Error(err))
		return "", 0
	}
	return fee.Currency, fee.Cost
}

func (o *LiveOrderMgr) applyHisOrder(sess *orm.Queries, ods []*orm.InOutOrder, od *banexg.Order, defTF string) ([]*orm.InOutOrder, *errs.Error) {
	isShort := od.PositionSide == banexg.PosSideShort
	isSell := od.Side == banexg.OdSideSell
	exs, err := orm.GetExSymbolCur(od.Symbol)
	if err != nil {
		return ods, err
	}
	feeName, feeCost := getFeeNameCost(od.Fee, od.Symbol, od.Type, od.Side, od.Filled, od.Average)
	price, amount, odTime := od.Average, od.Filled, od.Timestamp
	defTF = config.GetTakeOverTF(od.Symbol, defTF)

	if isShort == isSell {
		// 开多或开空
		if defTF == "" {
			log.Warn("take over job not found", zap.String("pair", od.Symbol), zap.String("stagy", config.TakeOverStgy))
			return ods, nil
		}
		tag := "开多"
		if isShort {
			tag = "开空"
		}
		log.Info(fmt.Sprintf("%s: price:%.5f, amount: %.5f, %v, fee: %.5f, %v id:%v",
			tag, price, amount, od.Type, feeCost, odTime, od.ID))
		iod := o.createInOutOd(exs, isShort, price, amount, od.Type, feeCost, feeName, odTime, orm.OdStatusClosed,
			od.ID, defTF)
		err = iod.Save(sess)
		if err != nil {
			return ods, err
		}
		ods = append(ods, iod)
	} else {
		// 平多或平空
		var part *orm.InOutOrder
		var res []*orm.InOutOrder
		for _, iod := range ods {
			if iod.Short != isShort {
				continue
			}
			amount, feeCost, part = o.tryFillExit(iod, amount, price, odTime, od.ID, od.Type, feeName, feeCost)
			err = part.Save(sess)
			if err != nil {
				return ods, err
			}
			tag := "平多"
			if isShort {
				tag = "平空"
			}
			log.Info(fmt.Sprintf("%v: price:%.5f, amount: %.5f, %v, %v id: %v",
				tag, price, part.Exit.Filled, od.Type, odTime, od.ID))
			if iod.Status < orm.InOutStatusFullExit {
				err = iod.Save(sess)
				if err != nil {
					return ods, err
				}
				res = append(res, iod)
			}
			if amount <= AmtDust {
				break
			}
		}
		if !od.ReduceOnly && amount > AmtDust {
			// 剩余数量，创建相反订单
			if defTF == "" {
				log.Warn("take over job not found", zap.String("pair", od.Symbol), zap.String("stagy", config.TakeOverStgy))
				return ods, nil
			}
			tag := "开多"
			if isShort {
				tag = "开空"
			}
			log.Info(fmt.Sprintf("%v: price:%.5f, amount: %.5f, %v, fee: %.5f %v id: %v",
				tag, price, amount, od.Type, feeCost, odTime, od.ID))
			iod := o.createInOutOd(exs, isShort, price, amount, od.Type, feeCost, feeName, odTime, orm.OdStatusClosed,
				od.ID, defTF)
			err = iod.Save(sess)
			if err != nil {
				return ods, err
			}
			ods = append(ods, iod)
		}
	}
	return ods, nil
}

func (o *LiveOrderMgr) createInOutOd(exs *orm.ExSymbol, short bool, average, filled float64, odType string,
	feeCost float64, feeName string, enterAt int64, entStatus int, entOdId string, defTF string) *orm.InOutOrder {
	notional := average * filled
	leverage, _ := exg.GetLeverage(exs.Symbol, notional)
	status := orm.InOutStatusPartEnter
	if entStatus == orm.OdStatusClosed {
		status = orm.InOutStatusFullEnter
	}
	stgVer, _ := strategy.Versions[config.TakeOverStgy]
	entSide := banexg.OdSideBuy
	if short {
		entSide = banexg.OdSideSell
	}
	od := &orm.InOutOrder{
		IOrder: &orm.IOrder{
			TaskID:    orm.TaskID,
			Symbol:    exs.Symbol,
			Sid:       exs.ID,
			Timeframe: defTF,
			Short:     short,
			Status:    int16(status),
			EnterTag:  core.EnterTagThird,
			InitPrice: average,
			QuoteCost: notional * float64(leverage),
			Leverage:  int32(leverage),
			EnterAt:   enterAt,
			Strategy:  config.TakeOverStgy,
			StgVer:    int32(stgVer),
		},
		Enter: &orm.ExOrder{
			TaskID:    orm.TaskID,
			Symbol:    exs.Symbol,
			Enter:     true,
			OrderType: odType,
			OrderID:   entOdId,
			Side:      entSide,
			CreateAt:  enterAt,
			Price:     average,
			Average:   average,
			Amount:    filled,
			Filled:    filled,
			Status:    int16(entStatus),
			Fee:       feeCost,
			FeeType:   feeName,
			UpdateAt:  enterAt,
		},
		DirtyMain:  true,
		DirtyEnter: true,
	}
	return od
}

func (o *LiveOrderMgr) createOdFromPos(pos *banexg.Position, defTF string) (*orm.InOutOrder, *errs.Error) {
	if defTF == "" {
		msg := fmt.Sprintf("take over job not found, %s %s", pos.Symbol, config.TakeOverStgy)
		return nil, errs.NewMsg(core.ErrBadConfig, msg)
	}
	exs, err := orm.GetExSymbolCur(pos.Symbol)
	if err != nil {
		return nil, err
	}
	average, filled, entOdType := pos.EntryPrice, pos.Contracts, config.OrderType
	isShort := pos.Side == banexg.PosSideShort
	//持仓信息没有手续费，直接从当前机器人订单类型推断手续费，可能和实际的手续费不同
	feeName, feeCost := getFeeNameCost(nil, pos.Symbol, "", pos.Side, pos.Contracts, pos.EntryPrice)
	tag := "开多"
	if isShort {
		tag = "开空"
	}
	log.Info(fmt.Sprintf("[仓]%v: price:%.5f, amount:%.5f, fee: %.5f", tag, average, filled, feeCost))
	enterAt := btime.TimeMS()
	entStatus := orm.OdStatusClosed
	iod := o.createInOutOd(exs, isShort, average, filled, entOdType, feeCost, feeName, enterAt, entStatus, "", defTF)
	return iod, nil
}

/*
tryFillExit
尝试平仓，用于从第三方交易中更新机器人订单的平仓状态
*/
func (o *LiveOrderMgr) tryFillExit(iod *orm.InOutOrder, filled, price float64, odTime int64, orderID, odType,
	feeName string, feeCost float64) (float64, float64, *orm.InOutOrder) {
	if iod.Enter.Filled == 0 {
		err := iod.LocalExit(core.ExitTagForceExit, iod.InitPrice, "not entered")
		if err != nil {
			log.Error("local exit no enter order fail", zap.String("key", iod.Key()), zap.Error(err))
		}
		return filled, feeCost, iod
	}
	var avaAmount float64
	var doCut = false // 是否应该分割一个小订单
	if iod.Exit != nil && iod.Exit.Amount > 0 {
		avaAmount = iod.Exit.Amount - iod.Exit.Filled
		doCut = avaAmount/iod.Exit.Amount < 0.99
	} else {
		avaAmount = iod.Enter.Filled
	}
	if !doCut && filled < avaAmount*0.99 {
		doCut = true
	}
	var part = iod
	fillAmt := min(avaAmount, filled)
	curPartRate := fillAmt / filled
	filled -= fillAmt
	if doCut {
		part = iod.CutPart(fillAmt, 0)
	}
	curFeeCost := feeCost * curPartRate
	feeCost -= curFeeCost
	if part.Exit == nil {
		exitSide := banexg.OdSideSell
		if part.Short {
			exitSide = banexg.OdSideBuy
		}
		part.Exit = &orm.ExOrder{
			TaskID:    orm.TaskID,
			InoutID:   part.ID,
			Symbol:    part.Symbol,
			Enter:     false,
			OrderType: odType,
			OrderID:   orderID,
			Side:      exitSide,
			CreateAt:  odTime,
			Price:     price,
			Average:   price,
			Amount:    part.Enter.Amount,
			Filled:    fillAmt,
			Status:    orm.OdStatusClosed,
			Fee:       curFeeCost,
			FeeType:   feeName,
			UpdateAt:  odTime,
		}
	} else {
		part.Exit.Filled = fillAmt
		part.Exit.OrderType = odType
		part.Exit.OrderID = orderID
		part.Exit.Price = price
		part.Exit.Average = price
		part.Exit.Status = orm.OdStatusClosed
		part.Exit.Fee = curFeeCost
		part.Exit.FeeType = feeName
		part.Exit.UpdateAt = odTime
	}
	part.DirtyExit = true
	part.ExitTag = core.ExitTagThird
	part.ExitAt = odTime
	part.Status = orm.InOutStatusFullExit
	part.DirtyMain = true
	return filled, feeCost, part
}

func (o *LiveOrderMgr) ProcessOrders(sess *orm.Queries, env *banta.BarEnv, enters []*strategy.EnterReq,
	exits []*strategy.ExitReq, edits []*orm.InOutEdit) ([]*orm.InOutOrder, []*orm.InOutOrder, *errs.Error) {
	ents, extOrders, err := o.OrderMgr.ProcessOrders(sess, env, enters, exits)
	if err != nil {
		return ents, extOrders, err
	}
	for _, edit := range edits {
		o.queue <- &OdQItem{
			Order:  edit.Order,
			Action: edit.Action,
		}
	}
	return ents, extOrders, nil
}

func makeAfterEnter(o *LiveOrderMgr) FuncHandleIOrder {
	return func(order *orm.InOutOrder) *errs.Error {
		if isFarLimit(order.Enter) {
			// 长时间难以成交的限价单，先不提交到交易所，防止资金占用
			orm.AddTriggerOd(order)
			return nil
		}
		o.queue <- &OdQItem{
			Order:  order,
			Action: "enter",
		}
		return nil
	}
}

func makeAfterExit(o *LiveOrderMgr) FuncHandleIOrder {
	return func(order *orm.InOutOrder) *errs.Error {
		o.queue <- &OdQItem{
			Order:  order,
			Action: "exit",
		}
		return nil
	}
}

func (o *LiveOrderMgr) ConsumeOrderQueue() {
	if o.isConsumeOrderQ {
		return
	}
	o.isConsumeOrderQ = true
	go func() {
		defer func() {
			o.isConsumeOrderQ = false
		}()
		for {
			var item *OdQItem
			select {
			case <-core.Ctx.Done():
				return
			case item = <-o.queue:
				break
			}
			var err *errs.Error
			switch item.Action {
			case "enter":
				err = o.execOrderEnter(item.Order)
			case "exit":
				err = o.execOrderExit(item.Order)
			default:
				editTriggerOd(item.Order, item.Action)
			}
			if err != nil {
				log.Error("ConsumeOrderQueue error", zap.String("action", item.Action), zap.Error(err))
			}
		}
	}()
}

func (o *LiveOrderMgr) WatchMyTrades() {
	if o.isWatchMyTrade {
		return
	}
	out, err := exg.Default.WatchMyTrades(nil)
	if err != nil {
		log.Error("WatchMyTrades fail", zap.Error(err))
		return
	}
	o.isWatchMyTrade = true
	go func() {
		defer func() {
			o.isWatchMyTrade = false
		}()
		for trade := range out {
			if _, ok := core.PairsMap[trade.Symbol]; !ok {
				// 忽略不处理的交易对
				continue
			}
			tradeKey := trade.Symbol + trade.ID
			if _, ok := o.doneTrades[tradeKey]; ok {
				// 交易已处理
				continue
			}
			odKey := trade.Symbol + trade.Order
			if _, ok := o.exgIdMap[odKey]; !ok {
				// 没有匹配订单，记录到unMatchTrades
				o.unMatchTrades[tradeKey] = &trade
				continue
			}
			if _, ok := o.doneKeys[odKey]; ok {
				// 订单已完成
				continue
			}
			iod := o.exgIdMap[odKey]
			err = o.updateByMyTrade(iod, &trade)
			if err != nil {
				log.Error("updateByMyTrade fail", zap.String("key", iod.Key()),
					zap.String("trade", trade.ID), zap.Error(err))
			}
			subOd := iod.Exit
			if iod.Short == (trade.Side == banexg.OdSideSell) {
				subOd = iod.Enter
			}
			err = o.consumeUnMatches(iod, subOd)
			if err != nil {
				log.Error("consumeUnMatches for WatchMyTrades fail", zap.String("key", iod.Key()),
					zap.Error(err))
			}
		}
	}()
}

func (o *LiveOrderMgr) TrialUnMatchesForever() {
	if !core.ProdMode() || o.isTrialUnMatches {
		return
	}
	o.isTrialUnMatches = true
	go func() {
		defer func() {
			o.isTrialUnMatches = false
		}()
		for {
			if !core.Sleep(time.Second * 3) {
				return
			}
			var oldTrades []*banexg.MyTrade
			expireMS := btime.TimeMS() - 1000
			for key, trade := range o.unMatchTrades {
				if trade.Timestamp < expireMS {
					oldTrades = append(oldTrades, trade)
					delete(o.unMatchTrades, key)
				}
			}
			unHandleNum := 0
			allowTakeOver := config.TakeOverStgy != ""
			for _, trade := range oldTrades {
				if o.exitByMyTrade(trade) {
					continue
				} else if allowTakeOver && o.traceExgOrder(trade) {
					continue
				}
				unHandleNum += 1
			}
			if unHandleNum > 0 {
				log.Warn(fmt.Sprintf("expired unmatch orders: %v", unHandleNum))
			}
		}
	}()
}

func (o *LiveOrderMgr) updateByMyTrade(od *orm.InOutOrder, trade *banexg.MyTrade) *errs.Error {
	isSell := trade.Side == banexg.OdSideSell
	isEnter := od.Short == isSell
	subOd := od.Exit
	dirtTag := "enter"
	if isEnter {
		subOd = od.Enter
		dirtTag = "exit"
	}
	if subOd.Status == orm.OdStatusClosed {
		log.Debug(fmt.Sprintf("%s %s complete, skip trade: %v", od.Key(), dirtTag, trade.ID))
		return nil
	}
	return o.applyMyTrade(od, subOd, trade)
}

func (o *LiveOrderMgr) execOrderEnter(od *orm.InOutOrder) *errs.Error {
	if od.ExitTag != "" {
		// 订单已取消，不提交到交易所
		return nil
	}
	odKey := od.Key()
	forceDelOd := func(err *errs.Error) {
		log.Error("del enter order", zap.String("key", odKey), zap.Error(err))
		sess, conn, err := orm.Conn(nil)
		if err != nil {
			log.Error("get db sess fail", zap.Error(err))
			return
		}
		defer conn.Release()
		err = sess.DelOrder(od)
		if err != nil {
			log.Error("del order fail", zap.String("key", odKey), zap.Error(err))
		}
	}
	var err *errs.Error
	if od.Enter.Amount == 0 {
		if od.QuoteCost == 0 {
			legalCost := od.GetInfoFloat64(orm.OdInfoLegalCost)
			if legalCost > 0 {
				_, quote, _, _ := core.SplitSymbol(od.Symbol)
				od.QuoteCost = Wallets.GetAmountByLegal(quote, legalCost)
				if od.QuoteCost == 0 {
					core.ForbidPairs[od.Symbol] = true
					forceDelOd(errs.NewMsg(core.ErrRunTime, "no available"))
					return nil
				}
			} else {
				msg := "QuoteCost is required for enter:"
				err = od.LocalExit(core.ExitTagFatalErr, 0, msg)
				if err != nil {
					log.Error("local exit order fail", zap.String("key", odKey), zap.Error(err))
				}
				return errs.NewMsg(core.ErrRunTime, msg+odKey)
			}
		}
		realPrice := core.GetPrice(od.Symbol)
		// 这里应使用市价计算数量，因传入价格可能和市价相差很大
		od.Enter.Amount, err = exg.PrecAmount(exg.Default, od.Symbol, od.QuoteCost/realPrice)
		if err != nil {
			forceDelOd(err)
			return nil
		}
	}
	err = o.submitExgOrder(od, true)
	if err != nil {
		msg := "submit order fail, local exit"
		log.Error(msg, zap.String("key", odKey), zap.Error(err))
		err = od.LocalExit(core.ExitTagFatalErr, 0, msg)
		if err != nil {
			log.Error("local exit order fail", zap.String("key", odKey), zap.Error(err))
		}
	}
	return nil
}

func (o *LiveOrderMgr) execOrderExit(od *orm.InOutOrder) *errs.Error {
	odKey := od.Key()
	if (od.Enter.Amount == 0 || od.Enter.Filled < od.Enter.Amount) && od.Enter.Status < orm.OdStatusClosed {
		// 可能尚未入场，或未完全入场
		if od.Enter.OrderID != "" {
			order, err := exg.Default.CancelOrder(od.Enter.OrderID, od.Symbol, nil)
			if err != nil {
				log.Error("cancel order fail", zap.String("key", odKey), zap.String("err", err.Short()))
			} else {
				err = o.updateOdByExgRes(od, true, order)
				if err != nil {
					log.Error("apply cancel res fail", zap.String("key", odKey), zap.Error(err))
				}
			}
		}
		if od.Enter.Filled == 0 {
			od.Status = orm.InOutStatusFullExit
			od.Exit.Price = od.Enter.Price
			od.DirtyMain = true
			od.DirtyExit = true
			sess, conn, err := orm.Conn(nil)
			if err != nil {
				return err
			}
			defer conn.Release()
			err = o.finishOrder(od, sess)
			if err != nil {
				return err
			}
			cancelTriggerOds(od)
			return nil
		}
		o.callBack(od, true)
	}
	return o.submitExgOrder(od, false)
}

func (o *LiveOrderMgr) submitExgOrder(od *orm.InOutOrder, isEnter bool) *errs.Error {
	subOd := od.Exit
	if isEnter {
		subOd = od.Enter
	}
	var subDirty bool
	defer func() {
		if subDirty {
			if isEnter {
				od.DirtyEnter = true
			} else {
				od.DirtyExit = true
			}
		}
	}()
	var err *errs.Error
	exchange := exg.Default
	leverage, maxLeverage := exg.GetLeverage(od.Symbol, od.QuoteCost)
	if isEnter && od.Leverage > 0 && od.Leverage != int32(leverage) {
		newLeverage := min(maxLeverage, int(od.Leverage))
		if newLeverage != leverage {
			_, err = exchange.SetLeverage(newLeverage, od.Symbol, nil)
			if err != nil {
				return err
			}
			// 此币种杠杆比较小，对应缩小金额
			rate := float64(newLeverage) / float64(od.Leverage)
			od.Leverage = int32(newLeverage)
			subOd.Amount *= rate
			od.QuoteCost *= rate
			od.DirtyMain = true
			subDirty = true
		}
	}
	if subOd.OrderType == "" {
		subOd.OrderType = config.OrderType
		subDirty = true
	}
	if subOd.Price == 0 && subOd.OrderType != banexg.OdTypeMarket {
		// 非市价单时，计算价格
		buyPrice, sellPrice := o.getLimitPrice(od.Symbol, config.LimitVolSecs)
		price := sellPrice
		if subOd.Side == banexg.OdSideBuy {
			price = buyPrice
		}
		subOd.Price, err = exg.PrecPrice(exchange, od.Symbol, price)
		if err != nil {
			return err
		}
		subDirty = true
	}
	if subOd.Amount == 0 {
		if isEnter {
			return errs.NewMsg(core.ErrRunTime, fmt.Sprintf("amount is required for %s", od.Key()))
		}
		subOd.Amount = od.Enter.Filled
		if subOd.Amount == 0 {
			// 没有入场，直接本地退出。
			od.Status = orm.InOutStatusFullExit
			subOd.Price = od.Enter.Price
			od.DirtyExit = true
			od.DirtyMain = true
			sess, conn, err := orm.Conn(nil)
			if err != nil {
				return err
			}
			defer conn.Release()
			err = od.Save(sess)
			if err != nil {
				return err
			}
			err = o.finishOrder(od, sess)
			if err != nil {
				return err
			}
			cancelTriggerOds(od)
			return nil
		}
	}
	side, amount, price := subOd.Side, subOd.Amount, subOd.Price
	params := map[string]interface{}{}
	if core.IsContract {
		params["positionSide"] = "LONG"
		if od.Short {
			params["positionSide"] = "SHORT"
		}
	}
	res, err := exchange.CreateOrder(od.Symbol, subOd.OrderType, side, amount, price, &params)
	if err != nil {
		return err
	}
	err = o.updateOdByExgRes(od, isEnter, res)
	if err != nil {
		return err
	}
	if isEnter {
		stopLoss := od.GetInfoFloat64(orm.OdInfoStopLoss)
		if stopLoss > 0 {
			editTriggerOd(od, "StopLoss")
		}
		takeProfit := od.GetInfoFloat64(orm.OdInfoTakeProfit)
		if takeProfit > 0 {
			editTriggerOd(od, "TakeProfit")
		}
	} else {
		// 平仓，取消关联订单
		cancelTriggerOds(od)
	}
	if subOd.Status == orm.OdStatusClosed {
		o.callBack(od, isEnter)
	}
	return nil
}

func (o *LiveOrderMgr) updateOdByExgRes(od *orm.InOutOrder, isEnter bool, res *banexg.Order) *errs.Error {
	subOd := od.Exit
	if isEnter {
		subOd = od.Enter
		od.DirtyEnter = true
	} else {
		od.DirtyExit = true
	}
	if subOd.OrderID != "" {
		// 如修改订单价格，order_id会变化
		o.doneKeys[od.Symbol+subOd.OrderID] = true
	}
	subOd.OrderID = res.ID
	idKey := od.Symbol + subOd.OrderID
	o.exgIdMap[idKey] = od
	if o.hasNewTrades(res) && subOd.UpdateAt < res.Timestamp {
		subOd.UpdateAt = res.Timestamp
		subOd.Amount = res.Amount
		if res.Filled > 0 {
			fillPrice := subOd.Price
			if res.Average > 0 {
				fillPrice = res.Average
			} else if res.Price > 0 {
				fillPrice = res.Price
			}
			subOd.Average = fillPrice
			subOd.Filled = res.Filled
			if res.Fee != nil && res.Fee.Cost > 0 {
				subOd.Fee = res.Fee.Cost
				subOd.FeeType = res.Fee.Currency
			}
		}
		if res.Status == "expired" || res.Status == "rejected" || res.Status == "closed" || res.Status == "canceled" {
			subOd.Status = orm.OdStatusClosed
			if subOd.Filled > 0 && subOd.Average > 0 {
				subOd.Price = subOd.Average
			}
			if res.Filled == 0 {
				if isEnter {
					// 入场订单，0成交，被关闭；整体状态为：完全退出
					od.Status = orm.InOutStatusFullExit
				} else {
					// 出场订单，0成交，被关闭，整体状态为：已入场
					od.Status = orm.InOutStatusFullEnter
				}
			} else {
				if isEnter {
					od.Status = orm.InOutStatusFullEnter
				} else {
					od.Status = orm.InOutStatusFullExit
				}
			}
			od.DirtyMain = true
		}
		if od.Status == orm.InOutStatusFullExit {
			sess, conn, err := orm.Conn(nil)
			if err != nil {
				return err
			}
			err = o.finishOrder(od, sess)
			conn.Release()
			if err != nil {
				return err
			}
		}
	}
	return o.consumeUnMatches(od, subOd)
}

func (o *LiveOrderMgr) hasNewTrades(res *banexg.Order) bool {
	if core.IsContract {
		// 期货市场未返回trades，直接认为需要更新
		return true
	}
	if len(res.Trades) == 0 {
		return false
	}
	for _, trade := range res.Trades {
		key := res.Symbol + trade.ID
		if _, ok := o.doneTrades[key]; !ok {
			o.doneTrades[key] = true
			return true
		}
	}
	return false
}

func (o *LiveOrderMgr) consumeUnMatches(od *orm.InOutOrder, subOd *orm.ExOrder) *errs.Error {
	for key, trade := range o.unMatchTrades {
		if trade.Symbol != od.Symbol || trade.Order != subOd.OrderID {
			continue
		}
		delete(o.unMatchTrades, key)
		if subOd.Status == orm.OdStatusClosed {
			continue
		}
		if _, ok := o.doneTrades[key]; ok {
			continue
		}
		err := o.updateByMyTrade(od, trade)
		if err != nil {
			return err
		}
	}
	return nil
}

type VolPrice struct {
	BuyPrice  float64
	SellPrice float64
	ExpireMS  int64
}

/*
getLimitPrice
获取等待指定秒数的大概限价单价格
*/
func (o *LiveOrderMgr) getLimitPrice(pair string, waitSecs int) (float64, float64) {
	key := fmt.Sprintf("%s_%s", pair, strconv.Itoa(waitSecs))
	cache, ok := o.volPrices[key]
	if ok && cache.ExpireMS > btime.TimeMS() {
		return cache.BuyPrice, cache.SellPrice
	}
	// 无效或过期，需要重新计算
	avgVol, lastVol, err := getPairMinsVol(pair, 5)
	if err != nil {
		log.Error("getPairMinsVol fail for getLimitPrice", zap.String("pair", pair), zap.Error(err))
	}
	secsFlt := float64(waitSecs)
	// 5分钟每秒成交量*等待秒数*2：这里最后乘2是以防成交量过低
	depth := min(avgVol/30*secsFlt, lastVol/60*secsFlt)
	book, err := exg.GetOdBook(pair)
	var buyPrice, sellPrice float64
	if err != nil {
		buyPrice, sellPrice = 0, 0
		log.Error("get odBook fail", zap.String("pair", pair), zap.Error(err))
	} else {
		buyPrice = book.LimitPrice(banexg.OdSideBuy, depth)
		sellPrice = book.LimitPrice(banexg.OdSideSell, depth)
	}
	// 价格缓存最长3s，最短传入的1/10
	expMS := min(3000, int64(waitSecs)*100)
	o.volPrices[key] = &VolPrice{
		BuyPrice:  buyPrice,
		SellPrice: sellPrice,
		ExpireMS:  btime.TimeMS() + expMS,
	}
	return buyPrice, sellPrice
}

/*
getPairMinsVol
获取一段时间内，每分钟平均成交量，以及最后一分钟成交量
此函数有缓存，每分钟更新
*/
func getPairMinsVol(pair string, num int) (float64, float64, *errs.Error) {
	cacheKey := fmt.Sprintf("%s_%v", pair, num)
	cache, ok := pairVolMap[cacheKey]
	curMs := btime.TimeMS()
	if ok && cache.ExpireMS > curMs {
		return cache.AvgVol, cache.LastVol, nil
	}
	calc := func() (float64, float64, *errs.Error) {
		exs, err := orm.GetExSymbolCur(pair)
		if err != nil {
			return 0, 0, err
		}
		bars, err := orm.AutoFetchOHLCV(exg.Default, exs, "1m", 0, 0, num, false)
		if err != nil {
			return 0, 0, err
		} else if len(bars) == 0 {
			return 0, 0, nil
		}
		sumVol := float64(0)
		for _, bar := range bars {
			sumVol += bar.Volume
		}
		lastMinVol := bars[len(bars)-1].Volume
		return sumVol / float64(len(bars)), lastMinVol, nil
	}
	avg, last, err := calc()
	expireMS := utils.AlignTfMSecs(curMs+60000, 60000)
	pairVolMap[cacheKey] = &PairValItem{AvgVol: avg, LastVol: last, ExpireMS: expireMS}
	return avg, last, err
}

/*
判断一个订单是否是长时间难以成交的限价单
*/
func isFarLimit(od *orm.ExOrder) bool {
	if od.Price == 0 || !strings.Contains(od.OrderType, banexg.OdTypeLimit) {
		// 非限价单，或没有指定价格，会很快成交
		return false
	}
	secs, rate, err := getSecsByLimit(od.Symbol, od.Side, od.Price)
	if err != nil {
		log.Error("getSecsByLimit for isFarLimit fail", zap.String("pair", od.Symbol),
			zap.String("side", od.Side), zap.Float64("price", od.Price), zap.Error(err))
		return false
	}
	if secs < config.PutLimitSecs && rate >= 0.8 {
		return false
	}
	return true
}

/*
VerifyTriggerOds
检查是否有可触发的限价单，如有，提交到交易所，应被每分钟调用
*/
func VerifyTriggerOds() {
	if len(orm.TriggerODs) == 0 {
		return
	}
	var resOds []*orm.InOutOrder
	for pair, ods := range orm.TriggerODs {
		if len(ods) == 0 {
			continue
		}
		var secsVol float64
		var book *banexg.OrderBook
		// 计算过去50分钟，平均成交量，以及最后一分钟成交量
		avgVol, lastVol, err := getPairMinsVol(pair, 50)
		if err == nil {
			secsVol = max(avgVol, lastVol) / 60
			if secsVol > 0 {
				book, err = exg.GetOdBook(pair)
			} else {
				err = errs.NewMsg(core.ErrRunTime, "getPairMinsVol vol is zero")
			}
		}
		if err != nil {
			log.Error("VerifyTriggerOds fail", zap.String("pair", pair), zap.Error(err))
			resOds = append(resOds, ods...)
			continue
		}
		var leftOds = make([]*orm.InOutOrder, 0, len(ods))
		for _, od := range ods {
			if od.Status == orm.InOutStatusFullExit {
				continue
			}
			subOd := od.Enter
			if od.Exit != nil {
				subOd = od.Exit
			}
			// 计算到指定价格，需要吃进的量，以及价格比例
			waitVol, rate := book.SumVolTo(subOd.Side, subOd.Price)
			// 最快成交时间 = 总吃进量 / 每秒成交量
			waitSecs := int(math.Round(waitVol / secsVol))
			if waitSecs < config.PutLimitSecs && rate >= 0.8 {
				resOds = append(resOds, od)
			} else {
				leftOds = append(leftOds, od)
			}
		}
		orm.TriggerODs[pair] = leftOds
	}
	for _, od := range resOds {
		if od.Status == orm.InOutStatusFullExit {
			continue
		}
		tag := "enter"
		if od.Exit != nil {
			tag = "exit"
		}
		OdMgrLive.queue <- &OdQItem{
			Order:  od,
			Action: tag,
		}
	}
}

/*
getSecsByLimit
根据目标价格，计算大概成交需要等待的时长。
*/
func getSecsByLimit(pair, side string, price float64) (int, float64, *errs.Error) {
	avgVol, lastVol, err := getPairMinsVol(pair, 50)
	if err != nil {
		return 0, 1, err
	}
	secsVol := max(avgVol, lastVol) / 60
	if secsVol == 0 {
		return 0, 1, nil
	}
	book, err := exg.GetOdBook(pair)
	if err != nil {
		return 0, 1, err
	}
	waitVol, rate := book.SumVolTo(side, price)
	return int(math.Round(waitVol / secsVol)), rate, nil
}

func editTriggerOd(od *orm.InOutOrder, prefix string) {
	orderId := od.GetInfoString(prefix + "OrderId")
	params := map[string]interface{}{}
	if core.IsContract {
		params[banexg.ParamPositionSide] = "LONG"
		if od.Short {
			params[banexg.ParamPositionSide] = "SHORT"
		}
	}
	trigPrice := od.GetInfoFloat64(prefix + "Price")
	if trigPrice > 0 {
		params["closePosition"] = true
		if prefix == "StopLoss" {
			params["stopLossPrice"] = trigPrice
		} else if prefix == "TakeProfit" {
			params["takeProfitPrice"] = trigPrice
		} else {
			log.Error("invalid trigger ", zap.String("prefix", prefix))
			return
		}
	}
	side := banexg.OdSideSell
	if od.Short {
		side = banexg.OdSideBuy
	}
	res, err := exg.Default.CreateOrder(od.Symbol, config.OrderType, side, od.Enter.Amount, trigPrice, &params)
	if err != nil {
		log.Error("put trigger order fail", zap.String("key", od.Key()), zap.Error(err))
	}
	if res != nil {
		od.SetInfo(prefix+"OrderId", res.ID)
		od.DirtyInfo = true
	}
	if orderId != "" && (res == nil || res.Status == "open") {
		_, err = exg.Default.CancelOrder(orderId, od.Symbol, nil)
		if err != nil {
			log.Error("cancel old trigger fail", zap.String("key", od.Key()), zap.Error(err))
		}
	}
}

/*
cancelTriggerOds
取消订单的关联订单。订单在平仓时，关联的止损单止盈单不会自动退出，需要调用此方法退出
*/
func cancelTriggerOds(od *orm.InOutOrder) {
	slOrder := od.GetInfoString(orm.OdInfoStopLossOrderId)
	tpOrder := od.GetInfoString(orm.OdInfoTakeProfitOrderId)
	odKey := od.Key()
	if slOrder != "" {
		_, err := exg.Default.CancelOrder(slOrder, od.Symbol, nil)
		if err != nil {
			log.Error("cancel stopLoss fail", zap.String("key", odKey), zap.Error(err))
		}
	}
	if tpOrder != "" {
		_, err := exg.Default.CancelOrder(tpOrder, od.Symbol, nil)
		if err != nil {
			log.Error("cancel takeProfit fail", zap.String("key", odKey), zap.Error(err))
		}
	}
}

func (o *LiveOrderMgr) finishOrder(od *orm.InOutOrder, sess *orm.Queries) *errs.Error {
	if od.Enter != nil && od.Enter.OrderID != "" {
		o.doneKeys[od.Symbol+od.Enter.OrderID] = true
	}
	if od.Exit != nil && od.Exit.OrderID != "" {
		o.doneKeys[od.Symbol+od.Exit.OrderID] = true
	}
	return o.OrderMgr.finishOrder(od, sess)
}

func WatchLeverages() {
	if !IsWatchAccConfig {
		return
	}
	if !core.IsContract {
		return
	}
	out, err := exg.Default.WatchAccountConfig(nil)
	if err != nil {
		log.Error("WatchLeverages error", zap.Error(err))
		return
	}
	IsWatchAccConfig = true
	go func() {
		defer func() {
			IsWatchAccConfig = false
		}()
		for range out {
			continue
		}
	}()
}

/*
CheckFatalStop
检查是否触发全局止损，此方法应通过cron定期调用
*/
func CheckFatalStop() {
	if core.NoEnterUntil >= btime.TimeMS() {
		return
	}
	for minsText, rate := range config.FatalStop {
		backMins, err := strconv.Atoi(minsText)
		if err != nil {
			log.Error("config.fatal_stop invalid: " + minsText)
			continue
		}
		lossRate := calcFatalLoss(backMins)
		if lossRate >= rate {
			lossPct := int(lossRate * 100)
			core.NoEnterUntil = btime.TimeMS() + int64(config.FatalStopHours)*3600*1000
			log.Error(fmt.Sprintf("%v分钟内损失%v, 禁止下单%v小时！", minsText, lossPct, config.FatalStopHours))
			break
		}
	}
}

/*
calcFatalLoss
计算系统级别最近n分钟内，账户余额损失百分比
*/
func calcFatalLoss(backMins int) float64 {
	minTimeMS := btime.TimeMS() - int64(backMins)*60000
	minTimeMS = min(minTimeMS, core.StartAt)
	sumProfit := float64(0)
	for i := len(orm.HistODs) - 1; i >= 0; i-- {
		od := orm.HistODs[i]
		if od.Enter.CreateAt < minTimeMS {
			break
		}
		sumProfit += od.Profit
	}
	if sumProfit >= 0 {
		return 0
	}
	lossVal := math.Abs(sumProfit)
	totalLegal := Wallets.TotalLegal(nil, false)
	return lossVal / (lossVal + totalLegal)
}

func (o *LiveOrderMgr) CleanUp() *errs.Error {
	return nil
}
