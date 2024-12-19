package config

import (
	"time"

	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func (a *CmdArgs) Init() {
	a.TimeFrames = utils.SplitSolid(a.RawTimeFrames, ",")
	a.Pairs = utils.SplitSolid(a.RawPairs, ",")
	a.Tables = utils.SplitSolid(a.RawTables, ",")
}

func (a *CmdArgs) ParseTimeZone() (*time.Location, *errs.Error) {
	if a.TimeZone != "" {
		loc, err_ := time.LoadLocation(a.TimeZone)
		if err_ != nil {
			err := errs.NewMsg(errs.CodeRunTime, "unsupport timezone: %s, %v", a.TimeZone, err_)
			return nil, err
		}
		return loc, nil
	} else {
		return banexg.LocUTC, nil
	}
}

func (a *CmdArgs) SetLog(showLog bool, handlers ...zapcore.Core) {
	log.Setup(a.LogLevel, a.Logfile, handlers...)
	core.LogFile = a.Logfile
	if showLog && a.Logfile != "" {
		log.Info("Log To", zap.String("path", a.Logfile))
	}
}
