package live

import (
	"github.com/banbox/banexg/bntp"
	"strings"
	"time"

	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/orm/ormo"
	"github.com/banbox/banbot/web/base"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

func regApiPub(api fiber.Router) {
	api.Post("/login", postLogin)
	api.Get("/ping", getPing)
}

func getPing(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status": "pong",
	})
}

func postLogin(c *fiber.Ctx) error {
	type LoginRequest struct {
		Username string `json:"username" validate:"required"`
		Password string `json:"password" validate:"required"`
	}
	var req = new(LoginRequest)
	if err := base.VerifyArg(c, req, base.ArgBody); err != nil {
		return err
	}

	users := config.GetApiUsers()
	for _, u := range users {
		if u.Username != req.Username || u.Password != req.Password {
			continue
		}
		expHours := u.ExpireHours
		if expHours == 0 {
			expHours = 168
		}
		token, err := createAuthToken(u.Username, config.APIServer.JWTSecretKey, expHours)
		if err != nil {
			return err
		}
		// 只返回正在运行的交易账户
		var accRoles = make(map[string]string)
		for acc, role := range u.AccRoles {
			task := ormo.GetTask(acc)
			if task != nil {
				accRoles[acc] = role
			}
		}
		return c.JSON(fiber.Map{
			"name":     config.Name,
			"token":    token,
			"accounts": u.AccRoles,
		})
	}
	return fiber.NewError(fiber.StatusUnauthorized, "invalid username or password")
}

type AuthClaims struct {
	User string `json:"user"`
	jwt.RegisteredClaims
}

func createAuthToken(user string, secret string, expHours float64) (string, error) {
	now := bntp.Now()
	claims := AuthClaims{
		User: user,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expHours*60) * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func AuthMiddleware(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tokenStr := c.Get("X-Authorization")
		if tokenStr == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "missing token")
		}
		tokenArr := strings.Split(tokenStr, " ")
		if len(tokenArr) != 2 || tokenArr[0] != "Bearer" {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid token")
		}
		token, err := jwt.Parse(tokenArr[1], func(token *jwt.Token) (interface{}, error) {
			// Validate the algorithm
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fiber.NewError(fiber.StatusUnauthorized, "invalid token")
			}
			return []byte(secret), nil
		})

		if err != nil || !token.Valid {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid token")
		}
		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			user := claims["user"]
			c.Locals("user", user)
			users := config.GetApiUsers()
			for _, u := range users {
				if u.Username == user {
					c.Locals("accounts", u.AccRoles)
					break
				}
			}
		}
		return c.Next()
	}
}
