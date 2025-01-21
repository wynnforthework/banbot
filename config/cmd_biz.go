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
	a.TimeFrames = utils.SplitSolid(a.RawTimeFrames, ",", true)
	a.Pairs = utils.SplitSolid(a.RawPairs, ",", true)
	a.Tables = utils.SplitSolid(a.RawTables, ",", true)
	if a.DataDir != "" {
		DataDir = a.DataDir
	}
	if a.InPath != "" {
		a.InPath = ParsePath(a.InPath)
	}
	if a.OutPath != "" {
		a.OutPath = ParsePath(a.OutPath)
	}
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
	logCfg := &log.Config{
		Stdout:            true,
		Format:            "text",
		Level:             a.LogLevel,
		Handlers:          handlers,
		DisableStacktrace: true,
	}
	if a.LogLevel == "" {
		logCfg.Level = "info"
	}
	if a.Logfile != "" {
		logCfg.File = &log.FileLogConfig{
			LogPath: a.Logfile,
		}
	}
	log.SetupLogger(logCfg)
	core.LogFile = a.Logfile
	if showLog && a.Logfile != "" {
		log.Info("Log To", zap.String("path", a.Logfile))
	}
}
