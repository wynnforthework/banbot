package api

import (
	"github.com/banbox/banbot/web/biz"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

func regApiWebsocket(api fiber.Router) {
	api.Get("/ohlcv", websocket.New(wsOHLCV))
}

func wsOHLCV(c *websocket.Conn) {
	biz.NewWsClient(c).HandleForever()
}
