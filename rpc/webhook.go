package rpc

import (
	"bytes"
	"fmt"
	"github.com/banbox/banbot/core"
	utils2 "github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/banbox/banexg/utils"
	"go.uber.org/zap"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type WebHook struct {
	name       string
	retryNum   int    // 重试次数
	retryDelay int    // 重试间隔
	disable    bool   // 是否禁用
	chlType    string // 渠道类型
	client     *http.Client
	wg         sync.WaitGroup
	doSendMsgs func([]map[string]string) int
	Config     map[string]interface{}
	MsgTypes   map[string]bool
	Keywords   []string
	Queue      chan map[string]string
}

const (
	MsgTypeStatus    = "status"
	MsgTypeException = "exception"
	MsgTypeStartUp   = "startup"

	MsgTypeEntry  = "entry"
	MsgTypeExit   = "exit"
	MsgTypeMarket = "market"
)

type IWebHook interface {
	GetName() string
	IsDisable() bool
	SetDisable(val bool)
	CleanUp()
	/*
		发送消息，payload是msg渲染后的待发送数据
	*/
	SendMsg(payload map[string]string) bool
	ConsumeForever()
}

func NewWebHook(name string, item map[string]interface{}) *WebHook {
	var msgTypes []string
	msgTypes = utils.GetMapVal(item, "msg_types", msgTypes)
	keywords := utils.GetMapVal(item, "keywords", []string{})
	res := &WebHook{
		name:       name,
		retryNum:   utils.GetMapVal(item, "retry_num", 0),
		retryDelay: utils.GetMapVal(item, "retry_delay", 1000),
		disable:    utils.GetMapVal(item, "disable", false),
		chlType:    utils.GetMapVal(item, "type", ""),
		Config:     item,
		MsgTypes:   make(map[string]bool),
		Keywords:   keywords,
		Queue:      make(chan map[string]string),
	}
	if msgTypes != nil {
		for _, val := range msgTypes {
			res.MsgTypes[val] = true
		}
	}
	return res
}

func (h *WebHook) GetName() string {
	return fmt.Sprintf("%s:%s", h.chlType, h.name)
}

func (h *WebHook) IsDisable() bool {
	return h.disable
}

func (h *WebHook) SetDisable(val bool) {
	h.disable = val
}

func (h *WebHook) SendMsg(payload map[string]string) bool {
	if h.disable {
		return false
	}
	if content, ok := payload["content"]; ok && len(h.Keywords) > 0 {
		match := false
		for _, word := range h.Keywords {
			if strings.Contains(content, word) {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	h.Queue <- payload
	h.wg.Add(1)
	return true
}

func (h *WebHook) CleanUp() {
	h.disable = true
	h.wg.Wait()
	if h.client != nil {
		h.client = nil
	}
	close(h.Queue)
}

func (h *WebHook) ConsumeForever() {
	if h.disable {
		return
	}
	name := h.GetName()
	log.Debug("start consume rpc for", zap.String("name", name))
	for {
		first := <-h.Queue
		var cache = []map[string]string{first}
	readCache:
		select {
		case item := <-h.Queue:
			cache = append(cache, item)
		default:
			break readCache
		}
		h.doSendRetry(cache)
	}
}

func (h *WebHook) doSendRetry(msgList []map[string]string) {
	sentNum, attempts, totalNum := 0, 0, len(msgList)
	for sentNum < totalNum && len(msgList) > 0 && attempts < h.retryNum {
		if attempts > 0 {
			core.Sleep(time.Duration(h.retryDelay) * time.Second)
		}
		attempts += 1
		curSent := h.doSendMsgs(msgList)
		sentNum += curSent
		h.wg.Add(0 - curSent)
		msgList = msgList[curSent:]
	}
	h.wg.Add(sentNum - totalNum)
}

func (h *WebHook) request(method, url, body string) *banexg.HttpRes {
	if h.client == nil {
		h.client = &http.Client{}
	}
	var reqBody io.Reader
	if body != "" {
		reqBody = bytes.NewBufferString(body)
	}
	req, err_ := http.NewRequest(method, url, reqBody)
	if err_ != nil {
		return &banexg.HttpRes{Error: errs.New(core.ErrRunTime, err_)}
	}
	return utils2.DoHttp(h.client, req)
}
