package entry

import (
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/live"
	"github.com/banbox/banbot/optmize"
	"github.com/banbox/banbot/orm"
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

func RunFixTF(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	return orm.SyncKlineTFs()
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
	err = data.RunSpider(config.SpiderAddr)
	core.RunExitCalls()
	return err
}

//func Load1mToDB(args *config.CmdArgs) *errs.Error {
//	core.SetRunMode(core.RunModeOther)
//	err := biz.SetupComs(args)
//	if err != nil {
//		return err
//	}
//	if args.InPath == "" {
//		return errs.NewMsg(errs.CodeParamRequired, "--in is required")
//	}
//	names, err := data.FindPathNames(args.InPath, ".zip")
//	if err != nil {
//		return err
//	}
//	totalNum := len(names) * core.StepTotal
//	pBar := utils.NewPrgBar(totalNum, "tickTo1m")
//	for _, name := range names {
//		fileInPath := filepath.Join(args.InPath, name)
//		err = data.ReadZipCSVs(fileInPath, pBar, readCsvKline)
//		if err != nil {
//			return err
//		}
//	}
//	pBar.Close()
//	return nil
//}

//func readCsvKline(inPath string, file *zip.File) *errs.Error {
//	cleanName := strings.Split(filepath.Base(file.Name), ".")[0]
//
//	ctx := context.Background()
//	sess, conn, err := orm.Conn(ctx)
//	if err != nil {
//		return err
//	}
//	defer conn.Release()
//	sess.InsertKLinesAuto("1m")
//}
