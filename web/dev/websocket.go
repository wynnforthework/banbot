package dev

import (
	"github.com/sasha-s/go-deadlock"

	"github.com/banbox/banexg/log"
	"github.com/banbox/banexg/utils"
	"github.com/gofiber/contrib/websocket"
	"go.uber.org/zap"
)

type ServerStatus struct {
	DirtyBin bool `json:"dirtyBin"`
	Building bool `json:"building"`
}

var (
	status  = ServerStatus{}
	clients = make(map[*WsClient]bool)
	wsLock  deadlock.RWMutex
)

type WsClient struct {
	Conn   *websocket.Conn
	remote string
	Tags   map[string]interface{}
}

func NewWsClient(c *websocket.Conn) *WsClient {
	client := &WsClient{
		Conn:   c,
		remote: c.RemoteAddr().String(),
		Tags:   make(map[string]interface{}),
	}

	wsLock.Lock()
	clients[client] = true
	wsLock.Unlock()

	return client
}

func (c *WsClient) HandleForever() {
	log.Debug("dev ws client joined", zap.String("ip", c.remote))
	for {
		if c.Conn == nil {
			break
		}
		mt, data, err := c.Conn.ReadMessage()
		if err != nil {
			log.Warn("ws read fail", zap.Error(err))
			c.Close()
			break
		}
		if mt == websocket.CloseMessage {
			c.Close()
			break
		}
		if mt != websocket.TextMessage {
			continue
		}

		var msg = map[string]interface{}{}
		err = utils.Unmarshal(data, &msg, utils.JsonNumAuto)
		if err != nil {
			log.Info("unexpedted ws msg", zap.String("str", string(data)))
			continue
		}

		action, ok := msg["action"]
		if !ok {
			log.Info("no action ws msg", zap.String("str", string(data)))
			continue
		}
		id := utils.GetMapVal(msg, "id", "")

		switch action {
		case "status":
			c.WriteMsg(map[string]interface{}{
				"id":   id,
				"type": "status",
				"data": status,
			})
		default:
			c.WriteMsg(map[string]interface{}{"error": "unsupported action"})
		}
	}
}

func (c *WsClient) WriteMsg(msg map[string]interface{}) {
	if c.Conn == nil {
		return
	}
	data, err := utils.Marshal(msg)
	if err != nil {
		log.Warn("marshal ws msg fail", zap.Error(err))
		return
	}
	err = c.Conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		log.Warn("write ws msg fail", zap.Error(err))
	}
}

func (c *WsClient) Close() {
	wsLock.Lock()
	delete(clients, c)
	wsLock.Unlock()

	_ = c.Conn.Close()
	c.Conn = nil
	log.Debug("dev ws client removed", zap.String("addr", c.remote))
}

func BroadcastWS(tag string, msg map[string]interface{}) {
	wsLock.RLock()
	defer wsLock.RUnlock()

	for client := range clients {
		if tag == "" {
			// 无tag时广播给所有客户端
			client.WriteMsg(msg)
		} else {
			// 有tag时只广播给包含此tag的客户端
			if _, ok := client.Tags[tag]; ok {
				client.WriteMsg(msg)
			}
		}
	}
}

func BroadcastStatus() {
	BroadcastWS("", map[string]interface{}{
		"type": "status",
		"data": status,
	})
}
