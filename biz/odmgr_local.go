package biz

import (
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	utils2 "github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/banbox/banta"
	"go.uber.org/zap"
	"strings"
)

const (
	netCost = 3
)

type LocalOrderMgr struct {
	OrderMgr
}

func InitLocalOrderMgr(callBack func(od *orm.InOutOrder, isEnter bool)) {
	for account := range config.Accounts {
		mgr, ok := accOdMgrs[account]
		if !ok {
			mgr = &LocalOrderMgr{
				OrderMgr{
					callBack: callBack,
					Account:  account,
				},
			}
			accOdMgrs[account] = mgr
		}
	}
}

func (o *LocalOrderMgr) ProcessOrders(sess *orm.Queries, env *banta.BarEnv, enters []*strategy.EnterReq,
	exits []*strategy.ExitReq, _ []*orm.InOutEdit) ([]*orm.InOutOrder, []*orm.InOutOrder, *errs.Error) {
	return o.OrderMgr.ProcessOrders(sess, env, enters, exits)
}

func (o *LocalOrderMgr) UpdateByBar(allOpens []*orm.InOutOrder, bar *banexg.PairTFKline) *errs.Error {
	err := o.OrderMgr.UpdateByBar(allOpens, bar)
	if err != nil {
		return err
	}
	if len(allOpens) == 0 || core.EnvReal {
		return nil
	}
	if core.IsContract {
		// 为合约更新此定价币的所有订单保证金和钱包情况
		_, _, code, _ := core.SplitSymbol(bar.Symbol)
		var orders []*orm.InOutOrder
		for _, od := range allOpens {
			_, _, odSettle, _ := core.SplitSymbol(od.Symbol)
			if odSettle == code {
				orders = append(orders, od)
			}
		}
		wallets := GetWallets(o.Account)
		err = wallets.UpdateOds(orders)
		if err != nil {
			return err
		}
	}
	var orders []*orm.InOutOrder
	var oldStas []int16
	for _, od := range allOpens {
		if od.Symbol == bar.Symbol {
			orders = append(orders, od)
			oldStas = append(oldStas, od.Status)
		}
	}
	_, err = o.fillPendingOrders(orders, bar)
	var chgOds = make([]*orm.InOutOrder, 0, len(orders))
	for i, od := range orders {
		if od.Status != oldStas[i] {
			chgOds = append(chgOds, od)
		}
	}
	if len(chgOds) > 0 {
		err = o.OrderMgr.UpdateByBar(chgOds, bar)
		if err != nil {
			return err
		}
	}
	return err
}

/*
fillPendingOrders
填充等待交易所响应的订单。不可用于实盘；可用于回测、模拟实盘等。
*/
func (o *LocalOrderMgr) fillPendingOrders(orders []*orm.InOutOrder, bar *banexg.PairTFKline) (int, *errs.Error) {
	affectNum := 0
	for _, od := range orders {
		if bar != nil && bar.TimeFrame != od.Timeframe {
			continue
		}
		var exOrder *orm.ExOrder
		if od.ExitTag != "" && od.Exit != nil && od.Exit.Status < orm.OdStatusClosed {
			exOrder = od.Exit
		} else if od.Enter.Status < orm.OdStatusClosed {
			exOrder = od.Enter
		} else {
			if od.ExitTag == "" {
				// 已入场完成，尚未出现出场信号，检查是否触发止损
				err := o.tryFillTriggers(od, &bar.Kline)
				if err != nil {
					return 0, err
				}
			}
			continue
		}
		odType := config.OrderType
		if exOrder.OrderType != "" {
			odType = exOrder.OrderType
		}
		price := exOrder.Price
		odTFSecs := utils2.TFToSecs(od.Timeframe)
		fillMS := btime.TimeMS() - int64(odTFSecs-netCost)*1000
		if bar == nil {
			price = core.GetPrice(od.Symbol)
		} else if odType == banexg.OdTypeLimit && exOrder.Price > 0 {
			if exOrder.Side == banexg.OdSideBuy {
				if price < bar.Low {
					continue
				} else if price > bar.Open {
					// 买价高于市价，以市价成交
					price = bar.Open
				}
			} else if exOrder.Side == banexg.OdSideSell {
				if price > bar.High {
					continue
				} else if price < bar.Open {
					// 卖价低于市价，以市价成交
					price = bar.Open
				}
			}
			barRate := simMarketRate(&bar.Kline, exOrder.Price, exOrder.Side == banexg.OdSideBuy)
			fillMS = bar.Time + int64(float64(odTFSecs)*barRate)*1000
		} else {
			// 按网络延迟，模拟成交价格，和开盘价接近
			rate := float64(netCost) / float64(odTFSecs)
			price = simMarketPrice(&bar.Kline, rate)
		}
		var err *errs.Error
		if exOrder.Enter {
			err = o.fillPendingEnter(od, price, fillMS)
			if err == nil {
				// 入场后可能立刻触发止损/止盈
				err = o.tryFillTriggers(od, &bar.Kline)
			}
		} else {
			err = o.fillPendingExit(od, price, fillMS)
		}
		if err != nil {
			return 0, err
		}
		affectNum += 1
	}
	// 强制平仓超时未成交的限价入场单
	curMS := btime.TimeMS()
	for _, od := range orders {
		if od.Status > orm.InOutStatusInit || od.Enter.Price == 0 ||
			!strings.Contains(od.Enter.OrderType, banexg.OdTypeLimit) {
			// 跳过已入场的以及非限价单
			continue
		}
		stopAfter := od.GetInfoInt64(orm.OdInfoStopAfter)
		if stopAfter > 0 && stopAfter <= curMS {
			err := od.LocalExit(core.ExitTagForceExit, od.InitPrice, "reach StopEnterBars", "")
			if err != nil {
				log.Error("local exit for StopEnterBars fail", zap.String("key", od.Key()), zap.Error(err))
			}
		}
	}
	return affectNum, nil
}

func (o *LocalOrderMgr) fillPendingEnter(od *orm.InOutOrder, price float64, fillMS int64) *errs.Error {
	wallets := GetWallets(o.Account)
	_, err := wallets.EnterOd(od)
	if err != nil {
		if err.Code == core.ErrLowFunds {
			err = od.LocalExit(core.ExitTagForceExit, od.InitPrice, err.Error(), "")
			o.onLowFunds()
			return err
		}
		return err
	}
	exchange := exg.Default
	market, err := exchange.GetMarket(od.Symbol)
	if err != nil {
		return err
	}
	entPrice, err := exchange.PrecPrice(market, price)
	if err != nil {
		return err
	}
	exOrder := od.Enter
	if exOrder.Amount == 0 {
		if od.Short && !core.IsContract {
			// 现货空单，必须给定数量
			return errs.NewMsg(core.ErrInvalidCost, "EnterAmount is required")
		}
		entAmount := od.QuoteCost / entPrice
		exOrder.Amount, err = exchange.PrecAmount(market, entAmount)
		if err != nil {
			log.Warn("prec enter amount fail", zap.Float64("amt", entAmount), zap.Error(err))
			err = od.LocalExit(core.ExitTagFatalErr, od.InitPrice, err.Error(), "")
			_, quote, _, _ := core.SplitSymbol(od.Symbol)
			wallets.Cancel(od.Key(), quote, 0, true)
			return err
		}
	}
	if exOrder.Price == 0 {
		exOrder.Price = entPrice
	}
	updateTime := fillMS
	exOrder.UpdateAt = updateTime
	if exOrder.CreateAt == 0 {
		exOrder.CreateAt = updateTime
	}
	if exOrder.OrderType == banexg.OdTypeLimit && updateTime-od.EnterAt < 60000 {
		// 以限价单入场，但很快成交的话，认为是市价单成交
		exOrder.OrderType = banexg.OdTypeMarket
	}
	if exOrder.Filled == 0 {
		// 将EnterAt更新为实际入场时间
		od.EnterAt = updateTime
	}
	exOrder.Filled = exOrder.Amount
	exOrder.Average = entPrice
	exOrder.Status = orm.OdStatusClosed
	err = od.UpdateFee(entPrice, true, false)
	if err != nil {
		return err
	}
	wallets.ConfirmOdEnter(od, entPrice)
	od.Status = orm.InOutStatusFullEnter
	od.DirtyEnter = true
	od.DirtyMain = true
	o.callBack(od, true)
	return nil
}

func (o *LocalOrderMgr) fillPendingExit(od *orm.InOutOrder, price float64, fillMS int64) *errs.Error {
	wallets := GetWallets(o.Account)
	exOrder := od.Exit
	wallets.ExitOd(od, exOrder.Amount)
	if exOrder.Filled == 0 {
		od.ExitAt = fillMS
	}
	exOrder.UpdateAt = fillMS
	exOrder.CreateAt = fillMS
	exOrder.Status = orm.OdStatusClosed
	exOrder.Price = price
	exOrder.Filled = exOrder.Amount
	exOrder.Average = price
	err := od.UpdateFee(price, false, false)
	if err != nil {
		return err
	}
	od.Status = orm.InOutStatusFullExit
	od.DirtyMain = true
	od.DirtyExit = true
	_ = o.finishOrder(od, nil)
	wallets.ConfirmOdExit(od, price)
	o.callBack(od, false)
	return nil
}

func (o *LocalOrderMgr) tryFillTriggers(od *orm.InOutOrder, bar *banexg.Kline) *errs.Error {
	slPrice := od.GetInfoFloat64(orm.OdInfoStopLoss)
	tpPrice := od.GetInfoFloat64(orm.OdInfoTakeProfit)
	if slPrice == 0 && tpPrice == 0 {
		return nil
	}
	slHit := od.GetInfoBool(orm.OdInfoStopLossHit)
	tpHit := od.GetInfoBool(orm.OdInfoTakeProfitHit)
	if !slHit {
		// 空单止损，最高价超过止损价触发
		// 多单止损，最低价跌破止损价触发
		slHit = slPrice > 0 && (od.Short && bar.High >= slPrice || !od.Short && bar.Low <= slPrice)
	} else if !tpHit {
		// 空单止盈，最低价跌破止盈价触发
		// 多单止盈，最高价突破止盈价触发
		tpHit = tpPrice > 0 && (od.Short && bar.Low <= tpPrice || !od.Short && bar.High >= tpPrice)
	}
	if !slHit && !tpHit {
		// 止损和止盈都未触发
		return nil
	}
	od.SetInfo(orm.OdInfoStopLossHit, slHit)
	od.SetInfo(orm.OdInfoTakeProfitHit, tpHit)
	tfSecs := float64(utils2.TFToSecs(od.Timeframe))
	getExcPrice := func(trigPrice, limit float64) float64 {
		if limit > 0 {
			if od.Short && limit < bar.Low || !od.Short && limit > bar.High {
				// 空单，平仓限价低于bar最低，不触发
				// 多单，平仓限价高于bar最高，不触发
				return -1
			}
			if od.Short && limit < trigPrice || !od.Short && limit > trigPrice {
				// 空单，平仓限价低于触发价，可能是限价单
				// 多单，平仓限价高于触发价，可能是限价单
				trigRate := simMarketRate(bar, trigPrice, od.Short)
				rate := simMarketRate(bar, limit, od.Short)
				if (rate-trigRate)*tfSecs > 30 {
					// 触发后，限价单超过30s成交，认为限价单
					return limit
				}
			}
		}
		return 0
	}
	var stopPrice float64
	var exitTag string
	if slHit {
		// 触发止损，计算执行价格
		stopPrice = getExcPrice(slPrice, od.GetInfoFloat64(orm.OdInfoStopLossLimit))
		exitTag = core.ExitTagStopLoss
	} else {
		// 触发止盈，计算执行价格
		stopPrice = getExcPrice(tpPrice, od.GetInfoFloat64(orm.OdInfoTakeProfitLimit))
		exitTag = core.ExitTagTakeProfit
	}
	if stopPrice < 0 {
		return nil
	}
	curMS := btime.TimeMS()
	rate := float64(0)
	odType := banexg.OdTypeMarket
	if stopPrice > 0 {
		odType = banexg.OdTypeLimit
		rate = simMarketRate(bar, stopPrice, od.Short)
	} else {
		// 市价止损，立刻卖出
		stopPrice = bar.Open
	}
	err := od.LocalExit(exitTag, stopPrice, "", odType)
	cutSecs := tfSecs * (1 - rate)
	od.ExitAt = curMS - int64(cutSecs*1000)
	od.DirtyMain = true
	if od.Exit != nil {
		od.Exit.UpdateAt = od.ExitAt
		od.Exit.CreateAt = od.ExitAt
		od.DirtyExit = true
	}
	_ = od.Save(nil)
	wallets := GetWallets(o.Account)
	wallets.ExitOd(od, od.Exit.Amount)
	wallets.ConfirmOdExit(od, od.Exit.Price)
	return err
}

func (o *LocalOrderMgr) onLowFunds() {
	// 如果余额不足，且没有入场的订单，则提前终止回测
	openNum := orm.OpenNum(o.Account, orm.InOutStatusPartEnter)
	if openNum > 0 {
		return
	}
	wallets := GetWallets(o.Account)
	value := wallets.TotalLegal(nil, false)
	if value < core.MinStakeAmount {
		log.Warn("wallet low funds, no open orders, stop backTest..")
		core.StopAll()
		core.BotRunning = false
	}
}

func (o *LocalOrderMgr) CleanUp() *errs.Error {
	sess, conn, err := orm.Conn(nil)
	if err != nil {
		return err
	}
	defer conn.Release()
	orders, err := o.ExitOpenOrders(sess, "", &strategy.ExitReq{
		Tag:  core.ExitTagBotStop,
		Dirt: core.OdDirtBoth,
	})
	if err != nil {
		return err
	}
	if len(orders) > 0 {
		_, err = o.fillPendingOrders(orders, nil)
		if err != nil {
			return err
		}
	}
	// 过滤未入场订单
	var validOds = make([]*orm.InOutOrder, 0, len(orm.HistODs))
	for _, od := range orm.HistODs {
		if od.Enter == nil || od.Enter.Filled == 0 {
			continue
		}
		validOds = append(validOds, od)
	}
	orm.HistODs = validOds
	return sess.DumpOrdersToDb()
}

func simMarketPrice(bar *banexg.Kline, rate float64) float64 {
	var (
		a, b, c, totalLen   float64
		aEndRate, bEndRate  float64
		start, end, posRate float64
	)

	openP := bar.Open
	highP := bar.High
	lowP := bar.Low
	closeP := bar.Close

	if openP <= closeP {
		// 阳线  一般是先下调走出下影线，然后上升到最高点，最后略微回撤，出现上影线
		a = openP - lowP
		b = highP - lowP
		c = highP - closeP
		totalLen = a + b + c
		if totalLen == 0 {
			return closeP
		}
		aEndRate = a / totalLen
		bEndRate = (a + b) / totalLen
		if rate <= aEndRate {
			start, end, posRate = openP, lowP, rate/aEndRate
		} else if rate <= bEndRate {
			start, end, posRate = lowP, highP, (rate-aEndRate)/(bEndRate-aEndRate)
		} else {
			start, end, posRate = highP, closeP, (rate-bEndRate)/(1-bEndRate)
		}
	} else {
		// 阴线  一般是先上升走出上影线，然后下降到最低点，最后略微回调，出现下影线
		a = highP - openP
		b = highP - lowP
		c = closeP - lowP
		totalLen = a + b + c
		if totalLen == 0 {
			return closeP
		}
		aEndRate = a / totalLen
		bEndRate = (a + b) / totalLen
		if rate <= aEndRate {
			start, end, posRate = openP, highP, rate/aEndRate
		} else if rate <= bEndRate {
			start, end, posRate = highP, lowP, (rate-aEndRate)/(bEndRate-aEndRate)
		} else {
			start, end, posRate = lowP, closeP, (rate-bEndRate)/(1-bEndRate)
		}
	}

	return start*(1-posRate) + end*posRate
}

func simMarketRate(bar *banexg.Kline, price float64, isBuy bool) float64 {
	if isBuy && price >= bar.Open || !isBuy && price <= bar.Open {
		// 立刻成交
		return 0
	}
	var (
		a, b, c, totalLen float64
	)

	openP := bar.Open
	highP := bar.High
	lowP := bar.Low
	closeP := bar.Close

	if openP <= closeP {
		// 阳线  一般是先下调走出下影线，然后上升到最高点，最后略微回撤，出现上影线
		a = openP - lowP   // 开盘~最低
		b = highP - lowP   // 最低~最高
		c = highP - closeP // 最高~收盘
		totalLen = a + b + c
		if totalLen == 0 {
			return 0
		}
		if isBuy {
			// 买单，在开盘~最低时触发
			return (openP - price) / totalLen
		} else {
			// 卖单，在最低~最高中触发
			return (a + price - lowP) / totalLen
		}
	} else {
		// 阴线  一般是先上升走出上影线，然后下降到最低点，最后略微回调，出现下影线
		a = highP - openP // 开盘~最高
		b = highP - lowP  // 最高~最低
		c = closeP - lowP // 最低~收盘
		totalLen = a + b + c
		if totalLen == 0 {
			return 0
		}
		if isBuy {
			// 买单，必然在最高~最低中触发
			return (a + highP - price) / totalLen
		} else {
			// 卖单，在开盘~最高中触发
			return (price - openP) / totalLen
		}
	}
}
