package dev

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

func regApiWebsocket(api fiber.Router) {
	api.Get("/ohlcv", websocket.New(wsOHLCV))
}

func wsOHLCV(c *websocket.Conn) {
	NewWsClient(c).HandleForever()
}
