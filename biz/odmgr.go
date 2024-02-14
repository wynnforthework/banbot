package biz

import (
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
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
	OdMgr IOrderMgr
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

type OrderMgr struct {
	wallet   *BanWallets
	data     data.IProvider
	callBack func(order *orm.InOutOrder, isEnter bool)
	queue    []*OdQItem
}

type OdQItem struct {
	Order  *orm.InOutOrder
	Action string
}

func allowOrderEnter(env *banta.BarEnv) bool {
	if _, ok := core.ForbidPairs[env.Symbol]; ok {
		return false
	}
	if core.RunMode == core.RunModeOther || len(orm.OpenODs) >= config.MaxOpenOrders {
		return false
	}
	if btime.TimeMS() < core.NoEnterUntil {
		log.Warn("any enter forbid", zap.String("pair", env.Symbol))
		return false
	}
	if core.RunMode != core.RunModeProd {
		return true
	}
	// 实盘订单提交到交易所，检查延迟不能超过80%
	rate := float64(btime.TimeMS()-env.TimeStop) / float64(env.TimeStop-env.TimeStart)
	return rate <= 0.8
}

func (o *OrderMgr) ProcessOrders(sess *orm.Queries, env *banta.BarEnv, enters []*strategy.EnterReq,
	exits []*strategy.ExitReq, edits []*orm.InOutEdit) ([]*orm.InOutOrder, []*orm.InOutOrder, *errs.Error) {
	var entOrders, extOrders []*orm.InOutOrder
	if len(enters) > 0 && allowOrderEnter(env) {
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
	for _, edit := range edits {
		o.queue = append(o.queue, &OdQItem{
			Order:  edit.Order,
			Action: edit.Action,
		})
	}
	return entOrders, extOrders, nil
}

func (o *OrderMgr) EnterOrder(sess *orm.Queries, env *banta.BarEnv, req *strategy.EnterReq, doCheck bool) (*orm.InOutOrder, *errs.Error) {
	isSpot := core.Market == banexg.MarketSpot
	if req.Short && isSpot {
		return nil, errs.NewMsg(core.ErrRunTime, "short oder is invalid for spot")
	}
	if doCheck && !allowOrderEnter(env) {
		return nil, nil
	}
	if req.Leverage == 0 && !isSpot {
		req.Leverage = config.Leverage
	}
	stgVer, _ := strategy.Versions[req.StgyName]
	odSide := banexg.OdSideBuy
	if req.Short {
		odSide = banexg.OdSideSell
	}
	od := &orm.InOutOrder{
		IOrder: &orm.IOrder{
			TaskID:    orm.TaskID,
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
			TaskID:    orm.TaskID,
			Symbol:    env.Symbol,
			Enter:     true,
			OrderType: req.OrderType,
			Side:      odSide,
			Price:     req.Limit,
			Amount:    req.Amount,
			Status:    orm.OdStatusInit,
		},
		Info:       map[string]interface{}{},
		DirtyMain:  true,
		DirtyEnter: true,
	}
	od.SetInfo(orm.OdInfoLegalCost, req.LegalCost)
	err := od.Save(sess)
	if err != nil {
		return nil, err
	}
	o.queue = append(o.queue, &OdQItem{
		Order:  od,
		Action: "enter",
	})
	return od, nil
}

func (o *OrderMgr) ExitOpenOrders(sess *orm.Queries, pairs string, req *strategy.ExitReq) ([]*orm.InOutOrder, *errs.Error) {
	// 筛选匹配的订单
	var matches []*orm.InOutOrder
	if req.OrderID > 0 {
		// 精确指定退出的订单ID
		od, ok := orm.OpenODs[req.OrderID]
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
		for _, od := range orm.OpenODs {
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
		exitAmount -= part.Enter.Amount
		result = append(result, part)
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
		base, quote, _, _ := utils2.SplitSymbol(od.Symbol)
		o.wallet.CutPart(srcKey, tgtKey, base, 1-req.ExitRate)
		o.wallet.CutPart(srcKey, tgtKey, quote, 1-req.ExitRate)
		req.ExitRate = 1
		return o.ExitOrder(sess, part, req)
	}
	od.SetExit(req.Tag, req.OrderType, req.Limit)
	err := od.Save(sess)
	if err != nil {
		return od, err
	}
	o.queue = append(o.queue, &OdQItem{
		Order:  od,
		Action: "exit",
	})
	return od, nil
}

/*
UpdateByBar
使用价格更新订单的利润等。可能会触发爆仓
*/
func (o *OrderMgr) UpdateByBar(allOpens []*orm.InOutOrder, bar *banexg.PairTFKline) *errs.Error {
	for _, od := range allOpens {
		if od.Symbol != bar.Symbol || od.Timeframe != bar.TimeFrame {
			continue
		}
		od.UpdateProfits(bar.Close)
	}
	return nil
}

func (o *OrderMgr) finishOrder(od *orm.InOutOrder, sess *orm.Queries) *errs.Error {
	od.UpdateProfits(0)
	return od.Save(sess)
}

func (o *OrderMgr) CleanUp() *errs.Error {
	return nil
}
