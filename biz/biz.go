package biz

import (
	"context"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/rpc"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func SetupComs(args *config.CmdArgs) *errs.Error {
	errs.PrintErr = utils.PrintErr
	ctx, cancel := context.WithCancel(context.Background())
	core.Ctx = ctx
	core.StopAll = cancel
	err := config.LoadConfig(args)
	if err != nil {
		return err
	}
	var logCores []zapcore.Core
	if core.LiveMode {
		logCores = append(logCores, rpc.NewExcNotify())
	}
	log.Setup(config.Args.Debug, config.Args.Logfile, logCores...)
	err = core.Setup()
	if err != nil {
		return err
	}
	err = exg.Setup()
	if err != nil {
		return err
	}
	err = orm.Setup()
	if err != nil {
		return err
	}
	err = goods.Setup()
	if err != nil {
		return err
	}
	return nil
}

func LoadRefreshPairs(addPairs []string) *errs.Error {
	pairTfScores, err := goods.RefreshPairList(addPairs)
	if err != nil {
		return err
	}
	var warms map[string]map[string]int
	warms, err = strategy.LoadStagyJobs(core.Pairs, pairTfScores)
	if err != nil {
		return err
	}
	core.PrintStagyGroups()
	return data.Main.SubWarmPairs(warms, true)
}

func AutoRefreshPairs() {
	var addPairs = make(map[string]bool)
	for account := range config.Accounts {
		openOds, lock := orm.GetOpenODs(account)
		lock.Lock()
		for _, od := range openOds {
			addPairs[od.Symbol] = true
		}
		lock.Unlock()
	}
	err := LoadRefreshPairs(utils.KeysOfMap(addPairs))
	if err != nil {
		log.Error("refresh pairs fail", zap.Error(err))
	}
}
