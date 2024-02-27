package rpc

import (
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	utils2 "github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/banbox/banexg/utils"
)

var (
	channels = make([]IWebHook, 0, 2)
)

func InitRPC() *errs.Error {
	return initWebHooks()
}

func initWebHooks() *errs.Error {
	if len(config.RPCChannels) == 0 {
		return nil
	}
	for name, item := range config.RPCChannels {
		chlType := utils.GetMapVal(item, "type", "")
		var channel IWebHook
		switch chlType {
		case "wework":
			channel = NewWeWork(name, item)
		default:
			return errs.NewMsg(core.ErrBadConfig, "RPCChannel not support: %v", chlType)
		}
		if channel.IsDisable() {
			continue
		}
		go channel.ConsumeForever()
		channels = append(channels, channel)
	}
	return nil
}

func SendMsg(msg map[string]interface{}) {
	if len(channels) == 0 {
		return
	}
	msgType := utils.GetMapVal(msg, "type", "")
	item, ok := config.Webhook[msgType]
	if !ok {
		log.Error(fmt.Sprintf("webhook for %v not found!", msgType))
		return
	}
	var payload = make(map[string]string)
	for key, val := range item {
		payload[key] = utils2.FormatWithMap(val, msg)
	}
	for _, chl := range channels {
		chl.SendMsg(payload)
	}
}

func CleanUp() {
	for _, chl := range channels {
		chl.SetDisable(true)
	}
	for _, chl := range channels {
		chl.CleanUp()
	}
	channels = make([]IWebHook, 0, 2)
}
