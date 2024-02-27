package entry

import (
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/live"
	"github.com/banbox/banbot/optmize"
	"github.com/banbox/banexg/errs"
)

func RunBackTest() *errs.Error {
	err := biz.SetupComs()
	if err != nil {
		return err
	}
	b := optmize.NewBackTest()
	b.Run()
	return nil
}

func RunTrade() *errs.Error {
	err := biz.SetupComs()
	if err != nil {
		return err
	}
	t := live.NewCryptoTrader()
	return t.Run()
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
