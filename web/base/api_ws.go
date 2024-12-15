package base

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

func RegApiWebsocket(api fiber.Router) {
	api.Get("/ohlcv", websocket.New(wsOHLCV))
}

func wsOHLCV(c *websocket.Conn) {
	NewWsClient(c).HandleForever()
}
