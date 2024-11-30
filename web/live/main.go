package live

import (
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/web/base"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"go.uber.org/zap"
	"strings"
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
	regApiPub(app.Group(""))
	regApiBiz(app.Group("/api", AuthMiddleware(cfg.JWTSecretKey)))

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
