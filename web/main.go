package web

import (
	"flag"
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/web/api"
	biz2 "github.com/banbox/banbot/web/biz"
	"github.com/banbox/banbot/web/cfg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"os"
)

func Run(args []string) error {
	var ag = &cfg.CmdArgs{}
	var f = flag.NewFlagSet("web", flag.ExitOnError)
	f.IntVar(&ag.Port, "port", 8000, "port to listen, default: 8000")
	f.StringVar(&ag.Host, "host", "0.0.0.0", "default: 0.0.0.0")
	f.StringVar(&ag.LogLevel, "level", "info", "log level, default: info")
	f.StringVar(&ag.TimeZone, "tz", "utc", "timezone, default: utc")
	f.StringVar(&ag.DataDir, "datadir", "", "Path to data dir.")
	f.Var(&ag.Configs, "config", "config path to use, Multiple -config options may be used")
	if args == nil {
		args = os.Args[1:]
	}
	err_ := f.Parse(args)
	if err_ != nil {
		return err_
	}
	// 初始化基础数据
	core.SetRunMode(core.RunModeLive)
	banArg := &config.CmdArgs{
		DataDir:  ag.DataDir,
		LogLevel: ag.LogLevel,
		TimeZone: ag.TimeZone,
		Configs:  ag.Configs,
	}
	var err2 *errs.Error
	if err2 = biz.SetupComs(banArg); err2 != nil {
		return err2
	}
	num := len(orm.GetAllExSymbols())
	log.Info("loaded symbols", zap.Int("num", num))

	// 新建web应用
	app := fiber.New(fiber.Config{
		AppName:      "banweb",
		ErrorHandler: api.ErrHandler,
	})

	// 注册路由
	api.RegRoutes(app)

	// 启动k线监听和websocket推送
	go biz2.RunReceiver()

	return app.Listen(fmt.Sprintf("%s:%v", ag.Host, ag.Port))
}
