package biz

import (
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	utils2 "github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/banbox/banexg/utils"
	"github.com/banbox/banta"
	"go.uber.org/zap"
	"slices"
	"strings"
)

var (
	accOdMgrs     = make(map[string]IOrderMgr)
	accLiveOdMgrs = make(map[string]*LiveOrderMgr)
)

type IOrderMgr interface {
	ProcessOrders(sess *orm.Queries, env *banta.BarEnv, enters []*strategy.EnterReq,
		exits []*strategy.ExitReq, edits []*orm.InOutEdit) ([]*orm.InOutOrder, []*orm.InOutOrder, *errs.Error)
	EnterOrder(sess *orm.Queries, env *banta.BarEnv, req *strategy.EnterReq, doCheck bool) (*orm.InOutOrder, *errs.Error)
	ExitOpenOrders(sess *orm.Queries, pairs string, req *strategy.ExitReq) ([]*orm.InOutOrder, *errs.Error)
	ExitOrder(sess *orm.Queries, od *orm.InOutOrder, req *strategy.ExitReq) (*orm.InOutOrder, *errs.Error)
	UpdateByBar(allOpens []*orm.InOutOrder, bar *banexg.PairTFKline) *errs.Error
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
	callBack   func(order *orm.InOutOrder, isEnter bool)
	afterEnter FuncHandleIOrder
	afterExit  FuncHandleIOrder
	Account    string
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

func allowOrderEnter(account string, env *banta.BarEnv) bool {
	if _, ok := core.ForbidPairs[env.Symbol]; ok {
		return false
	}
	if core.RunMode == core.RunModeOther {
		// 不涉及订单模式，禁止开单
		return false
	}
	openOds, lock := orm.GetOpenODs(account)
	lock.Lock()
	numOver := len(openOds) >= config.MaxOpenOrders
	lock.Unlock()
	if numOver {
		return false
	}
	stopUntil, _ := core.NoEnterUntil[account]
	if btime.TimeMS() < stopUntil {
		log.Warn("any enter forbid", zap.String("pair", env.Symbol))
		return false
	}
	if !core.LiveMode {
		// 回测模式，不检查延迟，直接允许
		return true
	}
	// 实盘订单提交到交易所，检查延迟不能超过80%
	rate := float64(btime.TimeMS()-env.TimeStop) / float64(env.TimeStop-env.TimeStart)
	return rate <= 0.8
}

func (o *OrderMgr) ProcessOrders(sess *orm.Queries, env *banta.BarEnv, enters []*strategy.EnterReq,
	exits []*strategy.ExitReq) ([]*orm.InOutOrder, []*orm.InOutOrder, *errs.Error) {
	var entOrders, extOrders []*orm.InOutOrder
	if len(enters) > 0 && allowOrderEnter(o.Account, env) {
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

func (o *OrderMgr) EnterOrder(sess *orm.Queries, env *banta.BarEnv, req *strategy.EnterReq, doCheck bool) (*orm.InOutOrder, *errs.Error) {
	isSpot := core.Market == banexg.MarketSpot
	if req.Short && isSpot {
		return nil, errs.NewMsg(core.ErrRunTime, "short oder is invalid for spot")
	}
	if doCheck && !allowOrderEnter(o.Account, env) {
		return nil, nil
	}
	if req.Leverage == 0 && !isSpot {
		req.Leverage = config.GetAccLeverage(o.Account)
	}
	stgVer, _ := strategy.Versions[req.StgyName]
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
			Leverage:  int32(req.Leverage),
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
			stopAfter := btime.TimeMS() + int64(req.StopBars*utils2.TFToSecs(od.Timeframe))*1000
			od.SetInfo(orm.OdInfoStopAfter, stopAfter)
		}
	}
	od.SetInfo(orm.OdInfoLegalCost, req.LegalCost)
	if req.StopLoss > 0 {
		od.SetInfo(orm.OdInfoStopLoss, req.StopLoss)
		if req.StopLossLimit > 0 {
			od.SetInfo(orm.OdInfoStopLossLimit, req.StopLossLimit)
		}
	}
	if req.TakeProfit > 0 {
		od.SetInfo(orm.OdInfoTakeProfit, req.TakeProfit)
		if req.TakeProfitLimit > 0 {
			od.SetInfo(orm.OdInfoTakeProfitLimit, req.TakeProfitLimit)
		}
	}
	od.DirtyInfo = true
	err := od.Save(sess)
	if err != nil {
		return od, err
	}
	if o.afterEnter != nil {
		err = o.afterEnter(od)
	}
	return od, err
}

func (o *OrderMgr) ExitOpenOrders(sess *orm.Queries, pairs string, req *strategy.ExitReq) ([]*orm.InOutOrder, *errs.Error) {
	// 筛选匹配的订单
	var matches []*orm.InOutOrder
	openOds, lock := orm.GetOpenODs(o.Account)
	if req.OrderID > 0 {
		// 精确指定退出的订单ID
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
				// 订单已退出
				continue
			}
			if !req.Force && !od.CanClose() {
				continue
			}
			if req.UnOpenOnly && od.Enter.Filled > 0 {
				continue
			}
			matches = append(matches, od)
		}
		lock.Unlock()
	}
	if len(matches) == 0 {
		return nil, nil
	}
	// 按未成交数量倒序
	slices.SortFunc(matches, func(a, b *orm.InOutOrder) int {
		unfillA := (a.Enter.Amount - a.Enter.Filled) * a.InitPrice
		unfillB := (b.Enter.Amount - b.Enter.Filled) * b.InitPrice
		return int((unfillB - unfillA) * 10)
	})
	// 计算要退出的数量
	allAmount := float64(0)
	for _, od := range matches {
		allAmount += od.Enter.Amount
		if od.Exit != nil {
			allAmount -= od.Exit.Amount
		}
	}
	exitAmount := allAmount
	if req.ExitRate > 0 {
		exitAmount = allAmount * req.ExitRate
	} else if req.Amount > 0 {
		exitAmount = req.Amount
	}
	var result []*orm.InOutOrder
	for _, od := range matches {
		dust := od.Enter.Amount * 0.01
		if exitAmount < dust {
			break
		}
		q := req.Clone()
		q.ExitRate = min(1, exitAmount/od.Enter.Amount)
		part, err := o.ExitOrder(sess, od, req)
		if err != nil {
			return result, err
		}
		if part != nil {
			exitAmount -= part.Enter.Amount
			result = append(result, part)
		}
	}
	return result, nil
}

func (o *OrderMgr) ExitOrder(sess *orm.Queries, od *orm.InOutOrder, req *strategy.ExitReq) (*orm.InOutOrder, *errs.Error) {
	if od.ExitTag != "" || (od.Exit != nil && od.Exit.Amount > 0) {
		// Exit一旦有值，表示全部退出
		return nil, nil
	}
	if req.ExitRate == 0 {
		if req.Amount == 0 {
			req.ExitRate = 1
		} else {
			req.ExitRate = req.Amount / od.Enter.Amount
		}
	}
	if req.ExitRate < 0.99 {
		// 要退出的部分不足99%，分割出一个小订单，用于退出。
		part := od.CutPart(req.ExitRate, 0)
		// 这里part的key和原始的一样，所以part作为src_key
		tgtKey, srcKey := od.Key(), part.Key()
		base, quote, _, _ := core.SplitSymbol(od.Symbol)
		wallets := GetWallets(o.Account)
		wallets.CutPart(srcKey, tgtKey, base, 1-req.ExitRate)
		wallets.CutPart(srcKey, tgtKey, quote, 1-req.ExitRate)
		req.ExitRate = 1
		err := od.Save(sess)
		if err != nil {
			log.Error("save cutPart parent order fail", zap.String("key", od.Key()), zap.Error(err))
		}
		return o.ExitOrder(sess, part, req)
	}
	odType := core.OrderTypeEnums[req.OrderType]
	if odType == "" {
		odType = config.OrderType
	}
	od.SetExit(req.Tag, odType, req.Limit)
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
使用价格更新订单的利润等。可能会触发爆仓
*/
func (o *OrderMgr) UpdateByBar(allOpens []*orm.InOutOrder, bar *banexg.PairTFKline) *errs.Error {
	for _, od := range allOpens {
		if od.Symbol != bar.Symbol || od.Timeframe != bar.TimeFrame || od.Status >= orm.InOutStatusFullExit {
			continue
		}
		od.UpdateProfits(bar.Close)
	}
	return nil
}

/*
finishOrder
sess 可为nil
实盘时内部会保存到数据库。
*/
func (o *OrderMgr) finishOrder(od *orm.InOutOrder, sess *orm.Queries) *errs.Error {
	od.UpdateProfits(0)
	return od.Save(sess)
}

func (o *OrderMgr) CleanUp() *errs.Error {
	return nil
}
