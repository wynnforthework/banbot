package main

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg/errs"
)

func RunBackTest() *errs.Error {
	err := biz.SetupComs()
	if err != nil {
		return err
	}
	ctx := context.Background()
	sess, conn, err := orm.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	orm.InitTask(sess)
	exchange, err := exg.Get()
	if err != nil {
		return err
	}
	// 交易对初始化
	err = orm.EnsureExgSymbols(exchange)
	if err != nil {
		return err
	}
	err = orm.InitListDates()
	if err != nil {
		return err
	}
	fmt.Printf("init ok")
	return nil
}

func RunTrade() *errs.Error {
	fmt.Println("in run trade")
	return nil
}

func RunDownData() *errs.Error {
	return nil
}

func RunDbCmd() *errs.Error {
	return nil
}

func RunSpider() *errs.Error {
	return nil
}
