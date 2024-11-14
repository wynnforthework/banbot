package api

import (
	"errors"
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"go.uber.org/zap"
	"strings"
)

var val *validator.Validate

func init() {
	val = validator.New(validator.WithRequiredStructEnabled())
}

func StartApi() *errs.Error {
	cfg := config.APIServer
	if cfg == nil || !cfg.Enable {
		return nil
	}
	app := fiber.New(fiber.Config{
		AppName:      "banbot",
		ErrorHandler: ErrHandler,
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

type (
	BadField struct {
		Field string
		Tag   string
		Value interface{}
	}

	BadFields struct {
		Items []*BadField
	}
)

const (
	ArgQuery = 1
	ArgBody  = 2
)

func VerifyArg(c *fiber.Ctx, out interface{}, from int) error {
	var err error
	if from == ArgQuery {
		err = c.QueryParser(out)
	} else if from == ArgBody {
		err = c.BodyParser(out)
	} else {
		return fmt.Errorf("unsupport arg source: %v", from)
	}

	if err != nil {
		return &fiber.Error{
			Code:    fiber.StatusBadRequest,
			Message: err.Error(),
		}
	}
	if err2 := Validate(out); err2 != nil {
		return err2
	}
	return nil
}

func Validate(data interface{}) *BadFields {
	err := val.Struct(data)
	if err != nil {
		var fields []*BadField
		for _, err := range err.(validator.ValidationErrors) {
			var elem BadField
			elem.Field = err.Field()
			elem.Tag = err.Tag()
			elem.Value = err.Value()
			fields = append(fields, &elem)
		}
		return &BadFields{Items: fields}
	}
	return nil
}

func (f *BadFields) Error() string {
	if f == nil {
		return ""
	}
	texts := make([]string, 0, len(f.Items))
	for _, it := range f.Items {
		texts = append(texts, fmt.Sprintf("[%s]: '%v', must %s", it.Field, it.Value, it.Tag))
	}
	return strings.Join(texts, ", ")
}

func ErrHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	errText := err.Error()

	var fieldErr *BadFields
	var fe *fiber.Error
	var banErr *errs.Error
	if errors.As(err, &fieldErr) {
		code = fiber.StatusBadRequest
	} else if errors.As(err, &fe) {
		code = fe.Code
	} else if errors.As(err, &banErr) {
		errText = banErr.Short()
	}

	fields := []zap.Field{zap.String("m", c.Method()), zap.String("url", c.OriginalURL()), zap.Error(err)}
	if code == fiber.StatusInternalServerError {
		log.Warn("server error", fields...)
	} else {
		log.Info("req fail", fields...)
	}

	c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)

	return c.Status(code).SendString(errText)
}
