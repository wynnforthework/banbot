package entry

import (
	_ "embed"
	"fmt"
	"path/filepath"

	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/live"
	"github.com/banbox/banbot/opt"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

func RunBackTest(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeBackTest)
	err := biz.SetupComsExg(args)
	if err != nil {
		return err
	}
	if args.OutPath == "" {
		hash, err := config.Data.HashCode()
		if err != nil {
			panic(err)
		}
		args.OutPath = fmt.Sprintf("$/backtest/%s", hash)
	}
	if args.Separate && len(config.RunPolicy) > 1 {
		log.Info("run backtest separately for policies", zap.Int("num", len(config.RunPolicy)))
		policyList := config.RunPolicy
		for i, item := range policyList {
			log.Info("start backtest", zap.Int("id", i+1), zap.String("name", item.Name))
			config.RunPolicy = []*config.RunPolicyConfig{item}
			outDir := runBackTest(fmt.Sprintf("%s%d", args.OutPath, i+1), "")
			err_ := utils.CopyDir(outDir, fmt.Sprintf("%s_%d", outDir, i+1))
			if err_ != nil {
				return errs.New(errs.CodeIOWriteFail, err_)
			}
		}
	} else {
		runBackTest(args.OutPath, args.PrgOut)
	}
	return nil
}

func runBackTest(outDir string, prgOut string) string {
	core.BotRunning = true
	biz.ResetVars()
	b := opt.NewBackTest(false, outDir)
	if prgOut != "" {
		lastSave := btime.UTCStamp()
		b.PBar.AddTrigger("", func(task string, rate float64) {
			curTime := btime.UTCStamp()
			if curTime-lastSave < 200 && rate < 1 {
				return
			}
			lastSave = curTime
			fmt.Printf("%s: %v\n", prgOut, rate)
		})
	}
	b.Run()
	return b.OutDir
}

func RunTrade(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeLive)
	err := biz.SetupComsExg(args)
	if err != nil {
		return err
	}
	core.BotRunning = true
	core.StartAt = btime.UTCStamp()
	t := live.NewCryptoTrader()
	return t.Run()
}

func RunDownData(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := biz.SetupComsExg(args)
	if err != nil {
		return err
	}
	pairs, err := goods.RefreshPairList()
	if err != nil {
		return err
	}
	if len(pairs) == 0 {
		log.Warn("no pairs to download")
		return nil
	}
	log.Info("start down kline for pairs", zap.Int("num", len(pairs)), zap.Strings("tfs", args.TimeFrames))
	exsMap := make(map[int32]*orm.ExSymbol)
	for _, pair := range pairs {
		exs, err := orm.GetExSymbolCur(pair)
		if err != nil {
			return err
		}
		exsMap[exs.ID] = exs
	}
	startMs, endMs := config.TimeRange.StartMS, config.TimeRange.EndMS
	for _, tf := range args.TimeFrames {
		err = orm.BulkDownOHLCV(exg.Default, exsMap, tf, startMs, endMs, 0, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func runExportData(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := biz.SetupComsExg(args)
	if err != nil {
		return err
	}
	return biz.ExportKlines(args, nil)
}

func runPurgeData(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := biz.SetupComsExg(args)
	if err != nil {
		return err
	}
	return biz.PurgeKlines(args)
}

func RunKlineCorrect(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	return orm.SyncKlineTFs(args, nil)
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
	err := biz.SetupComs(args)
	if err != nil {
		return err
	}
	return data.RunSpider(config.SpiderAddr)
}

func LoadKLinesToDB(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := biz.SetupComsExg(args)
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

func runInit(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	errs.PrintErr = utils.PrintErr
	err := biz.InitDataDir()
	if err != nil {
		return err
	}
	err = config.LoadConfig(args)
	if err != nil {
		return err
	}
	log.Info("init done")
	return nil
}

func runDataExport(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	if len(args.Configs) == 0 {
		return errs.NewMsg(errs.CodeParamRequired, "-config is required")
	}
	cfgPath := args.Configs[len(args.Configs)-1]
	args.Configs = args.Configs[:len(args.Configs)-1]
	err := biz.SetupComsExg(args)
	if err != nil {
		return err
	}
	if args.OutPath == "" {
		return errs.NewMsg(errs.CodeParamRequired, "-out is required")
	}
	cfgPath = config.ParsePath(cfgPath)
	return orm.ExportKData(cfgPath, args.OutPath, args.Concur, nil)
}

func runDataImport(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := biz.SetupComsExg(args)
	if err != nil {
		return err
	}
	if args.InPath == "" {
		return errs.NewMsg(errs.CodeParamRequired, "-in is required")
	}
	return orm.ImportData(args.InPath, args.Concur, nil)
}
