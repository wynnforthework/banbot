package biz

import (
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banta"
)

type LiveOrderMgr struct {
	OrderMgr
	queue []*OdQItem
}

type OdQItem struct {
	Order  *orm.InOutOrder
	Action string
}

func (o *LiveOrderMgr) ProcessOrders(sess *orm.Queries, env *banta.BarEnv, enters []*strategy.EnterReq,
	exits []*strategy.ExitReq, edits []*orm.InOutEdit) ([]*orm.InOutOrder, []*orm.InOutOrder, *errs.Error) {
	ents, extOrders, err := o.OrderMgr.ProcessOrders(sess, env, enters, exits)
	if err != nil {
		return ents, extOrders, err
	}
	for _, edit := range edits {
		o.queue = append(o.queue, &OdQItem{
			Order:  edit.Order,
			Action: edit.Action,
		})
	}
	return ents, extOrders, nil
}

func (o *LiveOrderMgr) EnterOrder(sess *orm.Queries, env *banta.BarEnv, req *strategy.EnterReq, doCheck bool) (*orm.InOutOrder, *errs.Error) {
	od, err := o.OrderMgr.EnterOrder(sess, env, req, doCheck)
	if err != nil {
		return od, err
	}
	o.queue = append(o.queue, &OdQItem{
		Order:  od,
		Action: "enter",
	})
	return od, nil
}

func (o *LiveOrderMgr) ExitOrder(sess *orm.Queries, od *orm.InOutOrder, req *strategy.ExitReq) (*orm.InOutOrder, *errs.Error) {
	exitOd, err := o.OrderMgr.ExitOrder(sess, od, req)
	if err != nil {
		return exitOd, err
	}
	o.queue = append(o.queue, &OdQItem{
		Order:  od,
		Action: "exit",
	})
	return exitOd, nil
}
