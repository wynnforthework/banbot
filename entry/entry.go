package entry

import (
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/live"
	"github.com/banbox/banbot/optmize"
	"github.com/banbox/banexg/errs"
)

func RunBackTest(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeBackTest)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	core.BotRunning = true
	b := optmize.NewBackTest()
	b.Run()
	return nil
}

func RunTrade(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeLive)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	core.BotRunning = true
	t := live.NewCryptoTrader()
	return t.Run()
}

func RunDownData(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	return nil
}

func RunDbCmd(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	return nil
}

func RunSpider(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeLive)
	if args.MaxPoolSize < 15 {
		// 爬虫端至少15个数据库会话
		args.MaxPoolSize = 15
	}
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	return data.RunSpider(config.SpiderAddr)
}
