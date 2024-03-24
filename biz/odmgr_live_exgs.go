package biz

import (
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

func bnbApplyMyTrade(o *LiveOrderMgr) FuncApplyMyTrade {
	return func(od *orm.InOutOrder, subOd *orm.ExOrder, trade *banexg.MyTrade) *errs.Error {
		if trade.State == "NEW" || trade.Timestamp < subOd.UpdateAt {
			// 收到的订单更新不一定按服务器端顺序。故早于已处理的时间戳的跳过
			return nil
		}
		if subOd.Enter {
			od.DirtyEnter = true
		} else {
			od.DirtyExit = true
		}
		subOd.UpdateAt = trade.Timestamp
		subOd.Amount = trade.Amount
		state := trade.State
		if state == "CANCELED" || state == "REJECTED" || state == "EXPIRED" || state == "EXPIRED_IN_MATCH" {
			subOd.Status = orm.OdStatusClosed
			if subOd.Enter {
				if subOd.Filled == 0 {
					od.Status = orm.InOutStatusFullExit
				} else {
					od.Status = orm.InOutStatusFullEnter
				}
				od.DirtyMain = true
			}
		} else if state == "FILLED" || state == "PARTIALLY_FILLED" {
			odStatus := orm.OdStatusPartOK
			if subOd.Filled == 0 {
				if subOd.Enter {
					od.EnterAt = trade.Timestamp
				} else {
					od.ExitAt = trade.Timestamp
				}
				od.DirtyMain = true
			}
			subOd.OrderType = trade.Type
			subOd.Filled = trade.Filled
			subOd.Average = trade.Average
			if state == "FILLED" {
				odStatus = orm.OdStatusClosed
				subOd.Price = trade.Average
				if subOd.Enter {
					od.Status = orm.InOutStatusFullEnter
				} else {
					od.Status = orm.InOutStatusFullExit
				}
				od.DirtyMain = true
			}
			subOd.Status = int16(odStatus)
			if trade.Fee != nil {
				subOd.FeeType = trade.Fee.Currency
				subOd.Fee = trade.Fee.Cost
			}
		} else {
			log.Error(fmt.Sprintf("unknown bnb order status: %s", state))
		}
		if od.Status == orm.InOutStatusFullExit {
			err := o.finishOrder(od, nil)
			if err != nil {
				return err
			}
			cancelTriggerOds(od)
			o.callBack(od, subOd.Enter)
		}
		return nil
	}
}

func bnbExitByMyOrder(o *LiveOrderMgr) FuncHandleMyOrder {
	return func(od *banexg.Order) bool {
		if od.Filled == 0 {
			return false
		}
		isShort := od.PositionSide == banexg.PosSideShort
		var openOds []*orm.InOutOrder
		accOpenOds, lock := orm.GetOpenODs(o.Account)
		lock.Lock()
		for _, iod := range accOpenOds {
			if iod.Short != isShort || iod.Symbol != od.Symbol || iod.Enter.Side == od.Side {
				continue
			}
			openOds = append(openOds, iod)
		}
		lock.Unlock()
		if len(openOds) == 0 {
			// 没有同方向，相反操作的可平仓订单
			return false
		}
		filled := od.Filled
		feeName, feeCost := "", float64(0)
		if od.Fee != nil {
			feeName = od.Fee.Currency
			feeCost = od.Fee.Cost
		}
		var err *errs.Error
		var part *orm.InOutOrder
		var doneParts []*orm.InOutOrder
		for _, iod := range openOds {
			lock = iod.Lock()
			filled, feeCost, part = o.tryFillExit(iod, filled, od.Average, od.Timestamp, od.ID, od.Type, feeName, feeCost)
			lock.Unlock()
			if part.Status == orm.InOutStatusFullExit {
				doneParts = append(doneParts, part)
			}
			if filled < AmtDust {
				break
			}
		}
		// 检查是否有剩余数量，创建相反订单
		createInv := !od.ReduceOnly && filled > AmtDust && config.TakeOverStgy != ""
		if len(doneParts) == 0 && !createInv {
			return true
		}
		sess, conn, err := orm.Conn(nil)
		if err != nil {
			log.Error("get sess fail bnbExitByMyOrder.tryFillExit", zap.Error(err))
			return true
		}
		defer conn.Release()
		for _, part = range doneParts {
			lock = part.Lock()
			err = o.finishOrder(part, sess)
			lock.Unlock()
			if err != nil {
				log.Error("finish order fail", zap.String("key", part.Key()), zap.Error(err))
			}
			log.Info("exit order by third", zap.String("acc", o.Account),
				zap.String("key", part.Key()), zap.String("id", od.ID))
			o.callBack(part, false)
		}
		if createInv {
			iod := o.makeInOutOd(sess, od.Symbol, isShort, od.Average, filled, od.Type, feeCost, feeName,
				od.Timestamp, orm.OdStatusClosed, od.ID)
			if iod != nil {
				o.callBack(iod, true)
			}
		}
		return true
	}
}

func (o *LiveOrderMgr) makeInOutOd(sess *orm.Queries, pair string, short bool, average, filled float64, odType string,
	feeCost float64, feeName string, enterAt int64, entStatus int, entOdId string) *orm.InOutOrder {
	exs, err := orm.GetExSymbolCur(pair)
	if err != nil {
		log.Error("get exSymbol fail", zap.Error(err))
		return nil
	}
	defTF := config.GetTakeOverTF(pair, "")
	if defTF != "" {
		log.Error("no stagy job found for trade", zap.String("pair", pair),
			zap.String("id", entOdId))
		return nil
	}
	iod := o.createInOutOd(exs, short, average, filled, odType, feeCost, feeName, enterAt,
		entStatus, entOdId, defTF)
	err = iod.Save(sess)
	if err != nil {
		log.Error("save third order fail", zap.String("key", iod.Key()), zap.Error(err))
		return nil
	}
	openOds, lock := orm.GetOpenODs(o.Account)
	lock.Lock()
	openOds[iod.ID] = iod
	lock.Unlock()
	return iod
}

func bnbTraceExgOrder(o *LiveOrderMgr) FuncHandleMyOrder {
	return func(od *banexg.Order) bool {
		if od.ReduceOnly || od.Status != banexg.OdStatusClosed {
			// 忽略只减仓订单  只对完全入场的尝试跟踪
			return false
		}
		isShort := od.PositionSide == banexg.PosSideShort
		if core.IsContract {
			if !isShort && od.Side == banexg.OdSideSell || isShort && od.Side == banexg.OdSideBuy {
				// 忽略平仓的订单
				return false
			}
		} else if od.Side == banexg.OdSideSell {
			// 现货市场卖出即平仓，忽略平仓
			return false
		}
		sess, conn, err := orm.Conn(nil)
		if err != nil {
			log.Error("get sess fail bnbTraceExgOrder", zap.Error(err))
			return true
		}
		defer conn.Release()
		feeName, feeCost := getFeeNameCost(od.Fee, od.Symbol, od.Type, od.Side, od.Amount, od.Average)
		iod := o.makeInOutOd(sess, od.Symbol, isShort, od.Average, od.Filled, od.Type, feeCost, feeName,
			od.Timestamp, orm.OdStatusClosed, od.ID)
		if iod != nil {
			o.callBack(iod, true)
		}
		return true
	}
}
