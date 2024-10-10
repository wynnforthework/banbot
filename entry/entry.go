package entry

import (
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
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
	if args.Separate && len(config.RunPolicy) > 1 {
		log.Info("run backtest separately for policies", zap.Int("num", len(config.RunPolicy)))
		policyList := config.RunPolicy
		for i, item := range policyList {
			log.Info("start backtest", zap.Int("id", i+1), zap.String("name", item.Name))
			config.RunPolicy = []*config.RunPolicyConfig{item}
			outDir := runBackTest()
			err_ := utils.CopyDir(outDir, fmt.Sprintf("%s_%d", outDir, i+1))
			if err_ != nil {
				return errs.New(errs.CodeIOWriteFail, err_)
			}
		}
	} else {
		runBackTest()
	}
	return nil
}

func runBackTest() string {
	core.BotRunning = true
	biz.ResetVars()
	b := optmize.NewBackTest(false, "")
	b.Run()
	core.RunExitCalls()
	return b.OutDir
}

func RunTrade(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeLive)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	core.BotRunning = true
	core.StartAt = btime.UTCStamp()
	t := live.NewCryptoTrader()
	err = t.Run()
	core.RunExitCalls()
	return err
}

func RunDownData(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	return nil
}

func RunKlineCorrect(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	return orm.SyncKlineTFs()
}

func RunKlineAdjFactors(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	return orm.CalcAdjFactors(args)
}

func RunSpider(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeLive)
	if args.MaxPoolSize < 15 {
		// At least 15 database sessions on the crawler side
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
	err = orm.InitExg(exg.Default)
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
