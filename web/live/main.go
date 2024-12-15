package live

import (
	"fmt"
	"strings"

	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/web/base"
	"github.com/banbox/banbot/web/ui"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"go.uber.org/zap"
)

func StartApi() *errs.Error {
	cfg := config.APIServer
	if cfg == nil || !cfg.Enable {
		return nil
	}
	app := fiber.New(fiber.Config{
		AppName:      "banbot",
		ErrorHandler: base.ErrHandler,
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins:     strings.Join(cfg.CORSOrigins, ", "),
		AllowMethods:     "*",
		AllowHeaders:     "*",
		AllowCredentials: len(cfg.CORSOrigins) > 0,
		ExposeHeaders:    "*",
	}))

	// register routes 注册路由
	base.RegApiKline(app.Group("/api/kline"))
	base.RegApiWebsocket(app.Group("/api/ws"))
	regApiBiz(app.Group("/api/bot", AuthMiddleware(cfg.JWTSecretKey)))
	regApiPub(app.Group("/api"))

	// 添加静态文件服务
	distFS, err_ := ui.BuildDistFS()
	if err_ != nil {
		return errs.New(errs.CodeRunTime, err_)
	}
	app.Use("/", filesystem.New(filesystem.Config{
		Root:         distFS,
		NotFoundFile: "404.html",
	}))

	addr := fmt.Sprintf("%s:%v", cfg.BindIPAddr, cfg.Port)
	log.Info("serve bot api at", zap.String("addr", addr))
	go func() {
		err_ := app.Listen(addr)
		if err_ != nil {
			log.Error("run api fail", zap.Error(err_))
		}
	}()
	return nil
}
