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
		defer func() {
			if subOd.Enter {
				od.DirtyEnter = true
			} else {
				od.DirtyExit = true
			}
		}()
		subOd.UpdateAt = trade.Timestamp
		subOd.Amount = trade.Amount
		state := trade.State
		if state == "CANCELED" || state == "REJECTED" || state == "EXPIRED" || state == "EXPIRED_IN_MATCH" {
			subOd.Status = orm.OdStatusClosed
		} else if state == "FILLED" || state == "PARTIALLY_FILLED" {
			odStatus := orm.OdStatusPartOK
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
			o.callBack(od, subOd.Enter)
		}
		return nil
	}
}

func bnbExitByMyTrade(o *LiveOrderMgr) FuncHandleMyTrade {
	return func(trade *banexg.MyTrade) bool {
		if trade.Filled == 0 {
			return false
		}
		isShort := trade.PosSide == banexg.PosSideShort
		var openOds []*orm.InOutOrder
		accOpenOds := orm.GetOpenODs(o.Account)
		for _, od := range accOpenOds {
			if od.Short != isShort || od.Symbol != trade.Symbol || od.Enter.Side == trade.Side {
				continue
			}
			openOds = append(openOds, od)
		}
		if len(openOds) == 0 {
			// 没有同方向，相反操作的可平仓订单
			return false
		}
		filled := trade.Filled
		feeName, feeCost := "", float64(0)
		if trade.Fee != nil {
			feeName = trade.Fee.Currency
			feeCost = trade.Fee.Cost
		}
		var err *errs.Error
		var part *orm.InOutOrder
		var doneParts []*orm.InOutOrder
		for _, od := range openOds {
			filled, feeCost, part = o.tryFillExit(od, filled, trade.Average, trade.Timestamp, trade.Order, trade.Type, feeName, feeCost)
			if part.Status == orm.InOutStatusFullExit {
				doneParts = append(doneParts, part)
			}
			if filled < AmtDust {
				break
			}
		}
		// 检查是否有剩余数量，创建相反订单
		createInv := !trade.ReduceOnly && filled > AmtDust && config.TakeOverStgy != ""
		if len(doneParts) == 0 && !createInv {
			return true
		}
		sess, conn, err := orm.Conn(nil)
		if err != nil {
			log.Error("get sess fail bnbExitByMyTrade.tryFillExit", zap.Error(err))
			return true
		}
		defer conn.Release()
		for _, part = range doneParts {
			err = o.finishOrder(part, sess)
			if err != nil {
				log.Error("finish order fail", zap.String("key", part.Key()), zap.Error(err))
			}
			log.Info("exit order by third", zap.String("key", part.Key()), zap.String("id", trade.ID))
			o.callBack(part, false)
		}
		if createInv {
			iod := o.makeInOutOd(sess, trade.Symbol, isShort, trade.Average, filled, trade.Type, feeCost, feeName,
				trade.Timestamp, orm.OdStatusClosed, trade.Order)
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
	openOds := orm.GetOpenODs(o.Account)
	openOds[iod.ID] = iod
	return iod
}

func bnbTraceExgOrder(o *LiveOrderMgr) FuncHandleMyTrade {
	return func(trade *banexg.MyTrade) bool {
		if trade.ReduceOnly || trade.State != "FILLED" {
			// 忽略只减仓订单  只对完全入场的尝试跟踪
			return false
		}
		isShort := trade.PosSide == banexg.PosSideShort
		if core.IsContract {
			if !isShort && trade.Side == banexg.OdSideSell || isShort && trade.Side == banexg.OdSideBuy {
				// 忽略平仓的订单
				return false
			}
		} else if trade.Side == banexg.OdSideSell {
			// 现货市场卖出即平仓，忽略平仓
			return false
		}
		sess, conn, err := orm.Conn(nil)
		if err != nil {
			log.Error("get sess fail bnbTraceExgOrder", zap.Error(err))
			return true
		}
		defer conn.Release()
		feeName, feeCost := getFeeNameCost(trade.Fee, trade.Symbol, trade.Type, trade.Side, trade.Amount, trade.Average)
		iod := o.makeInOutOd(sess, trade.Symbol, isShort, trade.Average, trade.Filled, trade.Type, feeCost, feeName,
			trade.Timestamp, orm.OdStatusClosed, trade.Order)
		if iod != nil {
			o.callBack(iod, true)
		}
		return true
	}
}
