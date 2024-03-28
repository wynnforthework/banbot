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
	"sync"
	"time"
)

type FuncApplyMyTrade = func(od *orm.InOutOrder, subOd *orm.ExOrder, trade *banexg.MyTrade) *errs.Error
type FuncHandleMyOrder = func(trade *banexg.Order) bool

type LiveOrderMgr struct {
	OrderMgr
	queue            chan *OdQItem
	doneKeys         map[string]bool            // 已完成的订单：symbol+orderId
	exgIdMap         map[string]*orm.InOutOrder // symbol+orderId: InOutOrder
	doneTrades       map[string]bool            // 已处理的交易：symbol+tradeId
	isWatchMyTrade   bool                       // 是否正在监听账户交易流
	isTrialUnMatches bool                       // 是否正在监听未匹配交易
	isConsumeOrderQ  bool                       // 是否正在从订单队列消费
	isWatchAccConfig bool                       // 是否正在监听杠杆倍数变化
	unMatchTrades    map[string]*banexg.MyTrade // 从ws收到的暂无匹配的订单的交易
	applyMyTrade     FuncApplyMyTrade           // 更新当前订单状态
	exitByMyOrder    FuncHandleMyOrder          // 尝试使用其他端操作的交易结果，更新当前订单状态
	traceExgOrder    FuncHandleMyOrder
}

type OdQItem struct {
	Order  *orm.InOutOrder
	Action string
}

const (
	AmtDust = 1e-8
)

var (
	pairVolMap     = map[string]*PairValItem{}
	volPrices      = map[string]*VolPrice{}
	lockPairVolMap sync.Mutex
	lockVolPrices  sync.Mutex
)

type PairValItem struct {
	AvgVol   float64
	LastVol  float64
	ExpireMS int64
}

func InitLiveOrderMgr(callBack func(od *orm.InOutOrder, isEnter bool)) {
	for account := range config.Accounts {
		mgr, ok := accLiveOdMgrs[account]
		if !ok {
			odMgr := newLiveOrderMgr(account, callBack)
			accLiveOdMgrs[account] = odMgr
			accOdMgrs[account] = odMgr
		} else {
			mgr.callBack = callBack
		}
	}
}

func newLiveOrderMgr(account string, callBack func(od *orm.InOutOrder, isEnter bool)) *LiveOrderMgr {
	res := &LiveOrderMgr{
		OrderMgr: OrderMgr{
			callBack: callBack,
			Account:  account,
		},
		queue:         make(chan *OdQItem, 1000),
		doneKeys:      map[string]bool{},
		exgIdMap:      map[string]*orm.InOutOrder{},
		doneTrades:    map[string]bool{},
		unMatchTrades: map[string]*banexg.MyTrade{},
	}
	res.afterEnter = makeAfterEnter(res)
	res.afterExit = makeAfterExit(res)
	if core.ExgName == "binance" {
		res.applyMyTrade = bnbApplyMyTrade(res)
		res.exitByMyOrder = bnbExitByMyOrder(res)
		res.traceExgOrder = bnbTraceExgOrder(res)
	} else {
		panic("unsupport exchange for LiveOrderMgr: " + core.ExgName)
	}
	return res
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
	task := orm.GetTask(o.Account)
	// 获取交易所挂单
	exOdList, err := exchange.FetchOpenOrders("", task.CreateAt, 1000, map[string]interface{}{
		banexg.ParamAccount: o.Account,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	exgOdMap := make(map[string]*banexg.Order)
	for _, od := range exOdList {
		exgOdMap[od.ID] = od
	}
	sess, conn, err := orm.Conn(nil)
	if err != nil {
		return nil, nil, nil, err
	}
	// 从数据库加载订单
	openOds, lock := orm.GetOpenODs(o.Account)
	orders, err := sess.GetOrders(orm.GetOrdersArgs{
		TaskID: task.ID,
		Status: 1,
		Limit:  1000,
	})
	if err != nil {
		conn.Release()
		return nil, nil, nil, err
	}
	// 查询任务的最近使用时间周期
	var pairLastTfs = make(map[string]string)
	if config.TakeOverStgy != "" {
		pairLastTfs, err = sess.GetHistOrderTfs(task.ID, config.TakeOverStgy)
		if err != nil {
			conn.Release()
			return nil, nil, nil, err
		}
	}
	var lastEntMS int64
	var openPairs = map[string]struct{}{}
	for _, od := range orders {
		if od.Status >= orm.InOutStatusFullExit {
			continue
		}
		lastEntMS = max(lastEntMS, od.EnterAt)
		err = o.restoreInOutOrder(od, exgOdMap)
		if err != nil {
			log.Error("restoreInOutOrder fail", zap.String("key", od.Key()), zap.Error(err))
		}
		if od.Status < orm.InOutStatusFullExit {
			lock.Lock()
			openOds[od.ID] = od
			lock.Unlock()
			openPairs[od.Symbol] = struct{}{}
		}
		err = od.Save(sess)
		if err != nil {
			log.Error("save order in SyncExgOrders fail", zap.String("key", od.Key()), zap.Error(err))
		}
	}
	// 这里用完就释放，防止长时间占用连接
	conn.Release()
	// 获取交易所仓位
	posList, err := exchange.FetchAccountPositions(nil, map[string]interface{}{
		banexg.ParamAccount: o.Account,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	posMap := utils.ArrToMap(posList, func(v *banexg.Position) string {
		openPairs[v.Symbol] = struct{}{}
		return v.Symbol
	})
	lock.Lock()
	var resOdList = make([]*orm.InOutOrder, 0, len(openOds))
	lock.Unlock()
	for pair := range openPairs {
		curOds := make([]*orm.InOutOrder, 0, 2)
		lock.Lock()
		for _, od := range openOds {
			if od.Symbol == pair {
				curOds = append(curOds, od)
			}
		}
		lock.Unlock()
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
		prevTF, _ := pairLastTfs[pair]
		curOds, err = o.syncPairOrders(pair, prevTF, longPos, shortPos, lastEntMS, curOds)
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
	lock.Lock()
	for key, od := range openOds {
		_, newHas := resMap[key]
		if !newHas {
			delList = append(delList, od)
		}
	}
	for key, od := range resMap {
		_, oldHas := openOds[key]
		if oldHas {
			oldList = append(oldList, od...)
		} else {
			newList = append(newList, od...)
			openOds[key] = od[0]
		}
	}
	lock.Unlock()
	if len(oldList) > 0 {
		log.Info(fmt.Sprintf("%s: 恢复%v个未平仓订单", o.Account, len(oldList)))
	}
	if len(newList) > 0 {
		log.Info(fmt.Sprintf("%s: 新开始跟踪%v个用户下单", o.Account, len(newList)))
	}
	err = orm.SaveDirtyODs(o.Account)
	if err != nil {
		log.Error("SaveDirtyODs fail", zap.Error(err))
	}
	return oldList, newList, delList, nil
}

/*
restoreInOutOrder
恢复订单状态
*/
func (o *LiveOrderMgr) restoreInOutOrder(od *orm.InOutOrder, exgOdMap map[string]*banexg.Order) *errs.Error {
	tryOd := od.Enter
	if od.Exit != nil {
		tryOd = od.Exit
	}
	var err *errs.Error
	if tryOd.Enter && tryOd.OrderID == "" && tryOd.Status == orm.OdStatusInit {
		// 订单未提交到交易所，且是入场订单
		tfMSecs := int64(utils.TFToSecs(od.Timeframe) * 1000)
		curMS := btime.TimeMS()
		notReachLimit := config.StopEnterBars == 0 || int((curMS-od.EnterAt)/tfMSecs) < config.StopEnterBars
		if notReachLimit && isFarLimit(tryOd) {
			orm.AddTriggerOd(o.Account, od)
		} else {
			return od.LocalExit(core.ExitTagForceExit, od.InitPrice, "重启取消未入场订单", "")
		}
	} else if tryOd.OrderID != "" && tryOd.Status != orm.OdStatusClosed {
		// 已提交到交易所，尚未完成
		exOd, ok := exgOdMap[tryOd.OrderID]
		if !ok {
			// 订单已取消或已成交，查询交易所订单
			exOd, err = exg.Default.FetchOrder(od.Symbol, tryOd.OrderID, map[string]interface{}{
				banexg.ParamAccount: o.Account,
			})
			if err != nil {
				return err
			}
		}
		if exOd != nil {
			err = o.updateOdByExgRes(od, tryOd.Enter, exOd)
			if err != nil {
				return err
			}
		}
	} else if !tryOd.Enter {
		// 平仓订单，这里不可能是已提交到交易所尚未完成，属于上一个else if
		err = o.tryExitEnter(od)
		if err != nil {
			return err
		}
		if od.Status >= orm.InOutStatusFullExit {
			// 订单已退出
			return nil
		}
		if tryOd.Status == orm.OdStatusClosed {
			od.Status = orm.InOutStatusFullExit
			od.DirtyMain = true
			return nil
		} else if tryOd.Status > orm.OdStatusInit {
			// 这里不应该走到
			log.Error("Exit Status Invalid", zap.String("key", od.Key()), zap.Int16("sta", tryOd.Status),
				zap.String("orderId", tryOd.OrderID))
		} else {
			// 这里OrderID一定为空，并且入场单数量一定有成交的。
			o.queue <- &OdQItem{
				Action: orm.OdActionExit,
				Order:  od,
			}
		}
	}
	return nil
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
		exOrders, err = exg.Default.FetchOrders(pair, sinceMs, 0, map[string]interface{}{
			banexg.ParamAccount: o.Account,
		})
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
			if odAmt == 0 {
				continue
			}
			if odAmt*iod.InitPrice < 1 {
				// TODO: 这里计算的quote价值，后续需要改为法币价值
				if iod.Status < orm.InOutStatusFullExit {
					msg := "订单没有入场仓位"
					err = iod.LocalExit(core.ExitTagFatalErr, iod.InitPrice, msg, "")
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
				err = iod.LocalExit(core.ExitTagFatalErr, iod.InitPrice, msg, "")
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
		log.Info(fmt.Sprintf("%s %s: price:%.5f, amount: %.5f, %v, fee: %.5f, %v id:%v",
			o.Account, tag, price, amount, od.Type, feeCost, odTime, od.ID))
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
			log.Info(fmt.Sprintf("%s %v: price:%.5f, amount: %.5f, %v, %v id: %v",
				o.Account, tag, price, part.Exit.Filled, od.Type, odTime, od.ID))
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
			log.Info(fmt.Sprintf("%s %v: price:%.5f, amount: %.5f, %v, fee: %.5f %v id: %v",
				o.Account, tag, price, amount, od.Type, feeCost, odTime, od.ID))
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
	leverage, _ := exg.GetLeverage(exs.Symbol, notional, o.Account)
	if leverage == 0 {
		leverage = config.GetAccLeverage(o.Account)
	}
	status := orm.InOutStatusPartEnter
	if entStatus == orm.OdStatusClosed {
		status = orm.InOutStatusFullEnter
	}
	stgVer, _ := strategy.Versions[config.TakeOverStgy]
	entSide := banexg.OdSideBuy
	if short {
		entSide = banexg.OdSideSell
	}
	taskId := orm.GetTaskID(o.Account)
	od := &orm.InOutOrder{
		IOrder: &orm.IOrder{
			TaskID:    taskId,
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
			TaskID:    taskId,
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
	log.Info(fmt.Sprintf("%s [仓]%v: price:%.5f, amount:%.5f, fee: %.5f", o.Account, tag, average, filled, feeCost))
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
		err := iod.LocalExit(core.ExitTagForceExit, iod.InitPrice, "not entered", "")
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
		taskId := orm.GetTaskID(o.Account)
		part.Exit = &orm.ExOrder{
			TaskID:    taskId,
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
		if edit.Action == orm.OdActionLimitEnter && isFarLimit(edit.Order.Enter) {
			orm.AddTriggerOd(o.Account, edit.Order)
			continue
		}
		o.queue <- &OdQItem{
			Order:  edit.Order,
			Action: edit.Action,
		}
	}
	return ents, extOrders, nil
}

func makeAfterEnter(o *LiveOrderMgr) FuncHandleIOrder {
	return func(order *orm.InOutOrder) *errs.Error {
		log.Info("NEW Enter", zap.String("acc", o.Account), zap.String("key", order.Key()))
		if isFarLimit(order.Enter) {
			// 长时间难以成交的限价单，先不提交到交易所，防止资金占用
			orm.AddTriggerOd(o.Account, order)
			return nil
		}
		o.queue <- &OdQItem{
			Order:  order,
			Action: orm.OdActionEnter,
		}
		return nil
	}
}

func makeAfterExit(o *LiveOrderMgr) FuncHandleIOrder {
	return func(order *orm.InOutOrder) *errs.Error {
		log.Info("Exit Order", zap.String("acc", o.Account), zap.String("key", order.Key()),
			zap.String("exitTag", order.ExitTag))
		o.queue <- &OdQItem{
			Order:  order,
			Action: orm.OdActionExit,
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
			o.handleOrderQueue(item.Order, item.Action)
		}
	}()
}

func (o *LiveOrderMgr) handleOrderQueue(od *orm.InOutOrder, action string) {
	var err *errs.Error
	lock := od.Lock()
	defer lock.Unlock()
	switch action {
	case orm.OdActionEnter:
		err = o.execOrderEnter(od)
	case orm.OdActionExit:
		err = o.execOrderExit(od)
	case orm.OdActionStopLoss, orm.OdActionTakeProfit:
		o.editTriggerOd(od, action)
	case orm.OdActionLimitEnter, orm.OdActionLimitExit:
		err = o.editLimitOd(od, action)
	default:
		log.Error("unknown od action", zap.String("action", action), zap.String("key", od.Key()))
		return
	}
	if err != nil {
		log.Error("ConsumeOrderQueue error", zap.String("action", action), zap.Error(err))
	}
	if od.IsDirty() {
		err = od.Save(nil)
		if err != nil {
			log.Error("save od for exg status fail", zap.String("key", od.Key()), zap.Error(err))
		}
	}
	if action == orm.OdActionEnter {
		if od.Enter.OrderID != "" && od.Status < orm.InOutStatusFullExit {
			log.Info("Enter Order Submitted", zap.String("acc", o.Account),
				zap.String("key", od.Key()))
		} else if od.Status >= orm.InOutStatusFullExit {
			log.Info("Enter Order Closed", zap.String("acc", o.Account),
				zap.String("key", od.Key()), zap.String("exitTag", od.ExitTag))
		}
	} else if action == orm.OdActionExit {
		if od.Exit.OrderID != "" {
			log.Info("Exit Order Submitted", zap.String("acc", o.Account),
				zap.String("key", od.Key()), zap.Int16("state", od.Status))
		} else if od.Status >= orm.InOutStatusFullExit {
			log.Info("Exit Order Closed", zap.String("acc", o.Account),
				zap.String("key", od.Key()), zap.String("exitTag", od.ExitTag))
		}
	}
}

func (o *LiveOrderMgr) WatchMyTrades() {
	if o.isWatchMyTrade {
		return
	}
	out, err := exg.Default.WatchMyTrades(map[string]interface{}{
		banexg.ParamAccount: o.Account,
	})
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
			o.handleMyTrade(trade)
		}
	}()
}

func (o *LiveOrderMgr) handleMyTrade(trade *banexg.MyTrade) {
	if _, ok := core.PairsMap[trade.Symbol]; !ok {
		// 忽略不处理的交易对
		return
	}
	tradeKey := trade.Symbol + trade.ID
	if _, ok := o.doneTrades[tradeKey]; ok {
		// 交易已处理
		return
	}
	odKey := trade.Symbol + trade.Order
	if _, ok := o.exgIdMap[odKey]; !ok {
		// 没有匹配订单，记录到unMatchTrades
		o.unMatchTrades[tradeKey] = trade
		return
	}
	if _, ok := o.doneKeys[odKey]; ok {
		// 订单已完成
		return
	}
	iod := o.exgIdMap[odKey]
	lock := iod.Lock()
	defer lock.Unlock()
	err := o.updateByMyTrade(iod, trade)
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
	if iod.IsDirty() {
		if iod.Status == orm.InOutStatusFullEnter {
			// 仅在完全入场后，下止损止盈单
			o.editTriggerOd(iod, orm.OdActionStopLoss)
			o.editTriggerOd(iod, orm.OdActionTakeProfit)
		}
		err = iod.Save(nil)
		if err != nil {
			log.Error("save od from myTrade fail", zap.String("key", iod.Key()), zap.Error(err))
		}
	}
}

func (o *LiveOrderMgr) TrialUnMatchesForever() {
	if !core.EnvReal || o.isTrialUnMatches {
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
			var pairTrades = make(map[string][]*banexg.MyTrade)
			expireMS := btime.TimeMS() - 1000
			for key, trade := range o.unMatchTrades {
				if trade.Timestamp >= expireMS {
					continue
				}
				odKey := trade.Symbol + trade.Order
				if iod, ok := o.exgIdMap[odKey]; ok {
					lock := iod.Lock()
					err := o.updateByMyTrade(iod, trade)
					lock.Unlock()
					if err != nil {
						log.Error("updateByMyTrade fail", zap.String("key", iod.Key()),
							zap.String("trade", trade.ID), zap.Error(err))
					}
					continue
				}
				odTrades, _ := pairTrades[odKey]
				pairTrades[odKey] = append(odTrades, trade)
				delete(o.unMatchTrades, key)
			}
			unHandleNum := 0
			allowTakeOver := config.TakeOverStgy != ""
			for _, trades := range pairTrades {
				exOd, err := exg.Default.MergeMyTrades(trades)
				if err != nil {
					log.Error("MergeMyTrades fail", zap.Int("num", len(trades)), zap.Error(err))
					continue
				}
				if o.exitByMyOrder(exOd) {
					continue
				} else if allowTakeOver && o.traceExgOrder(exOd) {
					continue
				}
				unHandleNum += 1
			}
			if unHandleNum > 0 {
				log.Warn(fmt.Sprintf("expired unmatch orders: %v", unHandleNum))
			}
			err := orm.SaveDirtyODs(o.Account)
			if err != nil {
				log.Error("SaveDirtyODs fail", zap.Error(err))
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
			wallets := GetWallets(o.Account)
			_, err = wallets.EnterOd(od)
			if err != nil {
				if err.Code == core.ErrLowFunds || err.Code == core.ErrInvalidCost {
					forceDelOd(err)
					return nil
				} else {
					msg := err.Short()
					err = od.LocalExit(core.ExitTagFatalErr, od.InitPrice, msg, "")
					if err != nil {
						log.Error("local exit order fail", zap.String("key", odKey), zap.Error(err))
					}
					return errs.NewMsg(core.ErrRunTime, msg+odKey)
				}
			}
		}
		realPrice := core.GetPrice(od.Symbol)
		// 这里应使用市价计算数量，因传入价格可能和市价相差很大
		od.Enter.Amount, err = exg.PrecAmount(exg.Default, od.Symbol, od.QuoteCost/realPrice)
		if err != nil {
			forceDelOd(err)
			return nil
		} else if od.Enter.Amount == 0 {
			forceDelOd(errs.NewMsg(core.ErrRunTime, "amount too small"))
			return nil
		}
	}
	err = o.submitExgOrder(od, true)
	if err != nil {
		msg := "submit order fail, local exit"
		log.Error(msg, zap.String("key", odKey), zap.Error(err))
		err = od.LocalExit(core.ExitTagFatalErr, od.InitPrice, msg, "")
		if err != nil {
			log.Error("local exit order fail", zap.String("key", odKey), zap.Error(err))
		}
	}
	return nil
}

func (o *LiveOrderMgr) tryExitEnter(od *orm.InOutOrder) *errs.Error {
	if od.Enter.Status == orm.OdStatusClosed {
		return nil
	}
	// 可能尚未入场，或未完全入场
	if od.Enter.OrderID != "" {
		order, err := exg.Default.CancelOrder(od.Enter.OrderID, od.Symbol, map[string]interface{}{
			banexg.ParamAccount: o.Account,
		})
		if err != nil {
			log.Error("cancel order fail", zap.String("key", od.Key()), zap.String("err", err.Short()))
		} else {
			err = o.updateOdByExgRes(od, true, order)
			if err != nil {
				log.Error("apply cancel res fail", zap.String("key", od.Key()), zap.Error(err))
			}
		}
	}
	if od.Enter.Filled == 0 {
		od.Status = orm.InOutStatusFullExit
		if od.Enter.Status < orm.OdStatusClosed {
			od.Enter.Status = orm.OdStatusClosed
			od.DirtyEnter = true
		}
		od.SetExit(core.ExitTagForceExit, "", od.Enter.Price)
		od.Exit.Status = orm.OdStatusClosed
		od.DirtyMain = true
		od.DirtyExit = true
		err := o.finishOrder(od, nil)
		if err != nil {
			return err
		}
		cancelTriggerOds(od)
		return nil
	} else if od.Enter.Status < orm.OdStatusClosed {
		od.Enter.Status = orm.OdStatusClosed
		od.DirtyEnter = true
	}
	if od.Enter.Filled > 0 {
		o.callBack(od, true)
	}
	return nil
}

func (o *LiveOrderMgr) execOrderExit(od *orm.InOutOrder) *errs.Error {
	err := o.tryExitEnter(od)
	if err != nil {
		return err
	}
	if od.Status >= orm.InOutStatusFullExit {
		return nil
	}
	return o.submitExgOrder(od, false)
}

func (o *LiveOrderMgr) submitExgOrder(od *orm.InOutOrder, isEnter bool) *errs.Error {
	subOd := od.Exit
	if isEnter {
		subOd = od.Enter
	}
	setDirty := func() {
		if isEnter {
			od.DirtyEnter = true
		} else {
			od.DirtyExit = true
		}
	}
	var err *errs.Error
	exchange := exg.Default
	leverage, maxLeverage := exg.GetLeverage(od.Symbol, od.QuoteCost, o.Account)
	if isEnter && od.Leverage > 0 && od.Leverage != int32(leverage) {
		newLeverage := min(maxLeverage, int(od.Leverage))
		if newLeverage != leverage {
			_, err = exchange.SetLeverage(newLeverage, od.Symbol, map[string]interface{}{
				banexg.ParamAccount: o.Account,
			})
			if err != nil {
				return err
			}
			// 此币种杠杆比较小，对应缩小金额
			rate := float64(newLeverage) / float64(od.Leverage)
			od.Leverage = int32(newLeverage)
			subOd.Amount *= rate
			od.QuoteCost *= rate
			od.DirtyMain = true
			setDirty()
		}
	}
	if subOd.OrderType == "" {
		subOd.OrderType = config.OrderType
		setDirty()
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
		setDirty()
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
			err = o.finishOrder(od, nil)
			if err != nil {
				return err
			}
			cancelTriggerOds(od)
			return nil
		}
	}
	side, amount, price := subOd.Side, subOd.Amount, subOd.Price
	params := map[string]interface{}{
		banexg.ParamAccount:       o.Account,
		banexg.ParamClientOrderId: od.ClientId(true),
	}
	if core.IsContract {
		params[banexg.ParamPositionSide] = "LONG"
		if od.Short {
			params[banexg.ParamPositionSide] = "SHORT"
		}
	}
	res, err := exchange.CreateOrder(od.Symbol, subOd.OrderType, side, amount, price, params)
	if err != nil {
		return err
	}
	err = o.updateOdByExgRes(od, isEnter, res)
	if err != nil {
		return err
	}
	if isEnter {
		if od.Status == orm.InOutStatusFullEnter {
			// 仅在完全入场后，下止损止盈单
			o.editTriggerOd(od, orm.OdActionStopLoss)
			o.editTriggerOd(od, orm.OdActionTakeProfit)
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
	if subOd.OrderID != "" && subOd.OrderID != res.ID {
		// 如修改订单价格，order_id会变化
		o.doneKeys[od.Symbol+subOd.OrderID] = true
	}
	subOd.OrderID = res.ID
	idKey := od.Symbol + subOd.OrderID
	o.exgIdMap[idKey] = od
	if o.hasNewTrades(res) && subOd.UpdateAt <= res.Timestamp {
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
			if subOd.Filled == 0 {
				if isEnter {
					od.EnterAt = res.Timestamp
				} else {
					od.ExitAt = res.Timestamp
				}
				od.DirtyMain = true
			}
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
			err := o.finishOrder(od, nil)
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
	lockVolPrices.Lock()
	cache, ok := volPrices[key]
	lockVolPrices.Unlock()
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
	lockVolPrices.Lock()
	volPrices[key] = &VolPrice{
		BuyPrice:  buyPrice,
		SellPrice: sellPrice,
		ExpireMS:  btime.TimeMS() + expMS,
	}
	lockVolPrices.Unlock()
	return buyPrice, sellPrice
}

/*
getPairMinsVol
获取一段时间内，每分钟平均成交量，以及最后一分钟成交量
此函数有缓存，每分钟更新
*/
func getPairMinsVol(pair string, num int) (float64, float64, *errs.Error) {
	cacheKey := fmt.Sprintf("%s_%v", pair, num)
	lockPairVolMap.Lock()
	cache, ok := pairVolMap[cacheKey]
	lockPairVolMap.Unlock()
	curMs := btime.TimeMS()
	if ok && cache.ExpireMS > curMs {
		return cache.AvgVol, cache.LastVol, nil
	}
	calc := func() (float64, float64, *errs.Error) {
		exs, err := orm.GetExSymbolCur(pair)
		if err != nil {
			return 0, 0, err
		}
		bars, err := orm.AutoFetchOHLCV(exg.Default, exs, "1m", 0, 0, num, false, nil)
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
	lockPairVolMap.Lock()
	pairVolMap[cacheKey] = &PairValItem{AvgVol: avg, LastVol: last, ExpireMS: expireMS}
	lockPairVolMap.Unlock()
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
仅实盘使用
*/
func VerifyTriggerOds() {
	for account := range config.Accounts {
		verifyAccountTriggerOds(account)
	}
}

func verifyAccountTriggerOds(account string) {
	triggerOds, lock := orm.GetTriggerODs(account)
	lock.Lock()
	isEmpty := len(triggerOds) == 0
	lock.Unlock()
	if isEmpty {
		return
	}
	var resOds []*orm.InOutOrder
	var copyTriggers = make(map[string]map[int64]*orm.InOutOrder)
	lock.Lock()
	for key, val := range triggerOds {
		copyTriggers[key] = val
	}
	lock.Unlock()
	for pair, ods := range copyTriggers {
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
			for _, od := range ods {
				resOds = append(resOds, od)
			}
			continue
		}
		var leftOds = make(map[int64]*orm.InOutOrder)
		for _, od := range ods {
			if od.Status >= orm.InOutStatusFullExit {
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
				leftOds[od.ID] = od
			}
		}
		lock.Lock()
		triggerOds[pair] = leftOds
		lock.Unlock()
	}
	odMgr := GetLiveOdMgr(account)
	for _, od := range resOds {
		if od.Status >= orm.InOutStatusFullExit {
			continue
		}
		tag := orm.OdActionEnter
		if od.Exit != nil {
			tag = orm.OdActionExit
		}
		odMgr.queue <- &OdQItem{
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

/*
CancelOldLimits
检查是否有超时未成交的入场限价单，有则取消。
*/
func CancelOldLimits() {
	for account := range config.Accounts {
		cancelAccountOldLimits(account)
	}
}

func cancelAccountOldLimits(account string) {
	var saveOds []*orm.InOutOrder
	openOds, lock := orm.GetOpenODs(account)
	lock.Lock()
	openArr := utils.ValsOfMap(openOds)
	lock.Unlock()
	odMgr := GetLiveOdMgr(account)
	for _, od := range openArr {
		if checkOldLimit(odMgr, od, account) {
			saveOds = append(saveOds, od)
		}
	}
	if len(saveOds) == 0 {
		return
	}
	// 有需要保存的订单
	sess, conn, err := orm.Conn(nil)
	if err != nil {
		log.Error("get sess to save old limits fail", zap.Error(err))
		return
	}
	defer conn.Release()
	for _, od := range saveOds {
		err = od.Save(sess)
		if err != nil {
			log.Error("save od fail", zap.String("key", od.Key()), zap.Error(err))
		}
	}
}

func checkOldLimit(odMgr *LiveOrderMgr, od *orm.InOutOrder, account string) bool {
	if od.Status > orm.InOutStatusPartEnter || od.Enter.Price == 0 ||
		!strings.Contains(od.Enter.OrderType, banexg.OdTypeLimit) {
		// 跳过已完全入场，或者非限价单
		return false
	}
	stopAfter := od.GetInfoInt64(orm.OdInfoStopAfter)
	if stopAfter > 0 && stopAfter <= btime.TimeMS() {
		lock := od.Lock()
		defer lock.Unlock()
		if od.Enter.OrderID != "" {
			res, err := exg.Default.CancelOrder(od.Enter.OrderID, od.Symbol, map[string]interface{}{
				banexg.ParamAccount: account,
			})
			if err != nil {
				log.Error("cancel old limit enters fail", zap.String("key", od.Key()), zap.Error(err))
			} else {
				err = odMgr.updateOdByExgRes(od, true, res)
				if err != nil {
					log.Error("apply cancel res fail", zap.String("key", od.Key()), zap.Error(err))
				}
			}
		}
		if od.Enter.Filled == 0 {
			// 尚未入场，直接退出
			err := od.LocalExit(core.ExitTagForceExit, od.InitPrice, "reach StopEnterBars", "")
			if err != nil {
				log.Error("local exit for StopEnterBars fail", zap.String("key", od.Key()), zap.Error(err))
			}
		} else {
			// 部分入场，置为已完全入场
			od.Enter.Status = orm.OdStatusClosed
			od.Status = orm.InOutStatusFullEnter
			od.DirtyMain = true
			od.DirtyEnter = true
		}
		return true
	}
	return false
}

func (o *LiveOrderMgr) editLimitOd(od *orm.InOutOrder, action string) *errs.Error {
	subOd := od.Enter
	if action == orm.OdActionLimitExit {
		subOd = od.Exit
	}
	exchange := exg.Default
	if core.Market != banexg.MarketLinear && core.Market != banexg.MarketInverse {
		// 现货，保证金，期权。先取消旧订单，再创建新订单
		_, err := exchange.CancelOrder(subOd.OrderID, od.Symbol, nil)
		if err != nil {
			return err
		}
		subOd.OrderID = ""
	}
	if subOd.OrderID == "" {
		if subOd.Enter {
			return o.execOrderEnter(od)
		} else {
			return o.execOrderExit(od)
		}
	}
	// 只有U本位 & 币本位，修改订单
	res, err := exchange.EditOrder(od.Symbol, subOd.OrderID, subOd.Side, subOd.Amount, subOd.Price, nil)
	if err != nil {
		return err
	}
	return o.updateOdByExgRes(od, subOd.Enter, res)
}

func (o *LiveOrderMgr) editTriggerOd(od *orm.InOutOrder, prefix string) {
	if od.Status >= orm.InOutStatusFullExit {
		return
	}
	trigPrice := od.GetInfoFloat64(prefix + "Price")
	limitPrice := od.GetInfoFloat64(prefix + "Limit")
	oldTrigPrice := od.GetInfoFloat64(prefix + "PriceOld")
	oldLimitPrice := od.GetInfoFloat64(prefix + "LimitOld")
	if trigPrice == oldTrigPrice && limitPrice == oldLimitPrice {
		// 和上次完全一样，无需重新提交
		return
	}
	od.SetInfo(prefix+"PriceOld", trigPrice)
	od.SetInfo(prefix+"LimitOld", limitPrice)
	orderId := od.GetInfoString(prefix + "OrderId")
	account := orm.GetTaskAcc(od.TaskID)
	if trigPrice <= 0 {
		// 未设置止损/止盈，或需要撤销
		if orderId != "" {
			_, err := exg.Default.CancelOrder(orderId, od.Symbol, map[string]interface{}{
				banexg.ParamAccount: account,
			})
			if err != nil {
				log.Error("cancel old trigger fail", zap.String("key", od.Key()), zap.Error(err))
			}
			od.SetInfo(prefix+"OrderId", nil)
		}
		return
	}
	params := map[string]interface{}{
		banexg.ParamAccount:       account,
		banexg.ParamClientOrderId: od.ClientId(true),
	}
	if core.IsContract {
		params[banexg.ParamPositionSide] = "LONG"
		if od.Short {
			params[banexg.ParamPositionSide] = "SHORT"
		}
	}
	var odType = banexg.OdTypeMarket
	if limitPrice > 0 {
		odType = banexg.OdTypeLimit
	} else {
		limitPrice = trigPrice
	}
	params[banexg.ParamClosePosition] = true
	if prefix == orm.OdActionStopLoss {
		params[banexg.ParamStopLossPrice] = trigPrice
	} else if prefix == orm.OdActionTakeProfit {
		params[banexg.ParamTakeProfitPrice] = trigPrice
	} else {
		log.Error("invalid trigger ", zap.String("prefix", prefix))
		return
	}
	side := banexg.OdSideSell
	if od.Short {
		side = banexg.OdSideBuy
	}
	res, err := exg.Default.CreateOrder(od.Symbol, odType, side, od.Enter.Amount, limitPrice, params)
	if err != nil {
		if err.BizCode == -2021 {
			// 止损止盈立刻成交，则市价平仓
			log.Warn("Order would immediately trigger, exit", zap.String("key", od.Key()))
			od.SetExit(prefix, banexg.OdTypeMarket, 0)
			err = o.execOrderExit(od)
			if err != nil {
				log.Error("exit order by trigger fail", zap.String("key", od.Key()), zap.Error(err))
			}
			err = od.Save(nil)
			if err != nil {
				log.Error("save order by trigger fail", zap.String("key", od.Key()), zap.Error(err))
			}
		} else {
			// 更新止损止盈失败时，不取消旧的止盈止损
			log.Error("put trigger order fail", zap.String("key", od.Key()), zap.Error(err))
		}
		return
	}
	if res != nil {
		od.SetInfo(prefix+"OrderId", res.ID)
		od.DirtyInfo = true
	}
	if orderId != "" && (res == nil || res.Status == "open") {
		_, err = exg.Default.CancelOrder(orderId, od.Symbol, map[string]interface{}{
			banexg.ParamAccount: account,
		})
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
	args := map[string]interface{}{
		banexg.ParamAccount: orm.GetTaskAcc(od.TaskID),
	}
	if slOrder != "" {
		_, err := exg.Default.CancelOrder(slOrder, od.Symbol, args)
		if err != nil {
			log.Error("cancel stopLoss fail", zap.String("key", odKey), zap.Error(err))
		}
	}
	if tpOrder != "" {
		_, err := exg.Default.CancelOrder(tpOrder, od.Symbol, args)
		if err != nil {
			log.Error("cancel takeProfit fail", zap.String("key", odKey), zap.Error(err))
		}
	}
}

/*
finishOrder
sess 可为nil
实盘时，内部会保存到数据库
*/
func (o *LiveOrderMgr) finishOrder(od *orm.InOutOrder, sess *orm.Queries) *errs.Error {
	if od.Enter != nil && od.Enter.OrderID != "" {
		o.doneKeys[od.Symbol+od.Enter.OrderID] = true
	}
	if od.Exit != nil && od.Exit.OrderID != "" {
		o.doneKeys[od.Symbol+od.Exit.OrderID] = true
	}
	log.Info("Finish Order", zap.String("acc", o.Account), zap.String("key", od.Key()),
		zap.String("tag", od.ExitTag))
	return o.OrderMgr.finishOrder(od, sess)
}

func (o *LiveOrderMgr) WatchLeverages() {
	if !core.IsContract || o.isWatchAccConfig {
		return
	}
	out, err := exg.Default.WatchAccountConfig(map[string]interface{}{
		banexg.ParamAccount: o.Account,
	})
	if err != nil {
		log.Error("WatchLeverages error", zap.Error(err))
		return
	}
	o.isWatchAccConfig = true
	go func() {
		defer func() {
			o.isWatchAccConfig = false
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
func MakeCheckFatalStop(maxIntv int) func() {
	return func() {
		for account := range config.Accounts {
			checkAccFatalStop(account, maxIntv)
		}
	}
}

func checkAccFatalStop(account string, maxIntv int) {
	stopUntil, _ := core.NoEnterUntil[account]
	if stopUntil >= btime.TimeMS() {
		return
	}
	sess, conn, err := orm.Conn(nil)
	if err != nil {
		log.Error("get db sess fail", zap.Error(err))
		return
	}
	defer conn.Release()
	minTimeMS := btime.TimeMS() - int64(maxIntv)*60000
	taskId := orm.GetTaskID(account)
	orders, err := sess.GetOrders(orm.GetOrdersArgs{
		TaskID:     taskId,
		Status:     2,
		CloseAfter: minTimeMS,
	})
	if err != nil {
		log.Error("get cur his orders fail", zap.Error(err))
		return
	}
	wallets := GetWallets(account)
	for backMins, rate := range config.FatalStop {
		lossRate := calcFatalLoss(wallets, orders, backMins)
		if lossRate >= rate {
			lossPct := int(lossRate * 100)
			core.NoEnterUntil[account] = btime.TimeMS() + int64(config.FatalStopHours)*3600*1000
			log.Error(fmt.Sprintf("%v: %v分钟内损失%v%%, 禁止下单%v小时！", account,
				backMins, lossPct, config.FatalStopHours))
			break
		}
	}
}

/*
calcFatalLoss
计算系统级别最近n分钟内，账户余额损失百分比
*/
func calcFatalLoss(wallets *BanWallets, orders []*orm.InOutOrder, backMins int) float64 {
	minTimeMS := btime.TimeMS() - int64(backMins)*60000
	minTimeMS = min(minTimeMS, core.StartAt)
	sumProfit := float64(0)
	for i := len(orders) - 1; i >= 0; i-- {
		od := orders[i]
		if od.Enter.CreateAt < minTimeMS {
			break
		}
		sumProfit += od.Profit
	}
	if sumProfit >= 0 {
		return 0
	}
	lossVal := math.Abs(sumProfit)
	totalLegal := wallets.TotalLegal(nil, false)
	return lossVal / (lossVal + totalLegal)
}

func (o *LiveOrderMgr) CleanUp() *errs.Error {
	return nil
}

func StartLiveOdMgr() {
	if !core.EnvReal {
		panic("StartLiveOdMgr for FakeEnv is forbidden:" + core.RunEnv)
	}
	for account := range config.Accounts {
		odMgr := GetLiveOdMgr(account)
		// 监听账户订单流
		odMgr.WatchMyTrades()
		// 跟踪用户下单
		odMgr.TrialUnMatchesForever()
		// 消费订单队列
		odMgr.ConsumeOrderQueue()
		// 监听杠杆倍数变化
		odMgr.WatchLeverages()
	}
}
