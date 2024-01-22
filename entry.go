package main

import (
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banexg/errs"
)

func RunBackTest() *errs.Error {
	err := biz.SetupComs()
	if err != nil {
		return err
	}
	err = orm.InitTask()
	if err != nil {
		return err
	}
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
	err = goods.RefreshPairList(nil)
	if err != nil {
		return err
	}
	err = strategy.LoadStagyJobs(core.Pairs, core.PairTfScores)
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
	err := biz.SetupComs()
	if err != nil {
		return err
	}
	return data.RunSpider(config.SpiderAddr)
}
