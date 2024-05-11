package entry

import (
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/live"
	"github.com/banbox/banbot/optmize"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"path/filepath"
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
	core.RunExitCalls()
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
	err = t.Run()
	core.RunExitCalls()
	return err
}

func RunDownData(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	return nil
}

func RunDbCmd(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	action := args.Action
	if action == "correct" {
		return orm.SyncKlineTFs()
	} else if action == "adj_factors" {
		return orm.CalcAdjFactors(args)
	} else {
		log.Warn("unsupport dbcmd", zap.String("action", action))
	}
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
	err = data.RunSpider(config.SpiderAddr)
	core.RunExitCalls()
	return err
}

func LoadKLinesToDB(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	if args.InPath == "" {
		return errs.NewMsg(errs.CodeParamRequired, "--in is required")
	}
	names, err := data.FindPathNames(args.InPath, ".zip")
	if err != nil {
		return err
	}
	var dirPath = names[0]
	names = names[1:]
	totalNum := len(names) * core.StepTotal
	pBar := utils.NewPrgBar(totalNum, "load1m")
	zArgs := []string{core.ExgName, core.Market, core.ContractType}
	for _, name := range names {
		fileInPath := filepath.Join(dirPath, name)
		err = data.ReadZipCSVs(fileInPath, pBar, biz.LoadZipKline, zArgs)
		if err != nil {
			return err
		}
	}
	pBar.Close()
	return nil
}
