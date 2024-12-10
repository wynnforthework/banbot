package dev

import (
	"flag"
	"fmt"
	"github.com/banbox/banbot/web/ui"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"os"

	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/web/base"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

func Run(args []string) error {
	var ag = &CmdArgs{}
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
		ErrorHandler: base.ErrHandler,
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
	}))

	// 注册API路由
	regApiKline(app.Group("/api/kline"))
	regApiWebsocket(app.Group("/api/ws"))
	regApiDev(app.Group("/api/dev"))

	// 添加静态文件服务
	distFS, err_ := ui.BuildDistFS()
	if err_ != nil {
		return err_
	}
	app.Use("/", filesystem.New(filesystem.Config{
		Root:         distFS,
		NotFoundFile: "404.html",
	}))

	// 启动k线监听和websocket推送
	go RunReceiver()

	return app.Listen(fmt.Sprintf("%s:%v", ag.Host, ag.Port))
}
