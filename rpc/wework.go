package rpc

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banexg/log"
	"github.com/banbox/banexg/utils"
	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

/**
获取AccessToken
https://developer.work.weixin.qq.com/document/path/91039
企业微信推送消息
https://developer.work.weixin.qq.com/document/path/90236
*/

type WeWork struct {
	*WebHook
	agentId     string
	corpId      string
	corpSecret  string
	accessToken string
	expireAt    int64
}

const (
	urlBase = "https://qyapi.weixin.qq.com"
)

func NewWeWork(name string, item map[string]interface{}) *WeWork {
	hook := NewWebHook(name, item)
	res := &WeWork{
		WebHook:    hook,
		agentId:    utils.GetMapVal(item, "agent_id", ""),
		corpId:     utils.GetMapVal(item, "corp_id", ""),
		corpSecret: utils.GetMapVal(item, "corp_secret", ""),
	}
	if res.corpId == "" || res.corpSecret == "" || res.agentId == "" {
		panic(name + ": `corp_id`, `corp_secret`, `agent_id` is required")
	}
	res.doSendMsgs = makeDoSendMsg(res)
	return res
}

func (w *WeWork) getToken() string {
	curTime := btime.TimeMS()
	if w.expireAt > curTime {
		// 这里accessToken可能有效，也可能为空。为空表示上次获取失败，当前处于等待禁止重试阶段
		return w.accessToken
	}
	// 1分钟仅允许重试一次请求token
	w.expireAt = curTime + 60000
	name := w.GetName()
	url := fmt.Sprintf("%s/cgi-bin/gettoken?corpid=%s&corpsecret=%s", urlBase, w.corpId, w.corpSecret)
	rsp := w.request("GET", url, "")
	if rsp.Error != nil {
		log.Error("wework get token fail", zap.String("name", name), zap.Error(rsp.Error))
		return ""
	}
	var res = make(map[string]interface{})
	err_ := sonic.UnmarshalString(rsp.Content, &res)
	if err_ != nil {
		log.Error("wework parse rsp fail", zap.String("name", name), zap.Error(rsp.Error),
			zap.String("body", rsp.Content))
		return ""
	}
	w.accessToken = utils.GetMapVal(res, "access_token", "")
	expireSecs := utils.GetMapVal(res, "expires_in", 0)
	if expireSecs > 0 {
		w.expireAt = curTime + int64(expireSecs*1000)
	}
	return w.accessToken
}

func makeDoSendMsg(h *WeWork) func([]map[string]string) int {
	return func(msgList []map[string]string) int {
		token := h.getToken()
		url := fmt.Sprintf("%s/cgi-bin/message/send?access_token=%s", urlBase, token)
		touser := utils.GetMapVal(h.Config, "touser", "")
		toparty := utils.GetMapVal(h.Config, "toparty", "")
		totag := utils.GetMapVal(h.Config, "totag", "")
		sentNum := 0
		for _, msg := range msgList {
			content, _ := msg["content"]
			if content == "" {
				log.Error("wework get empty msg, skip")
				sentNum += 1
				continue
			}
			var body = map[string]interface{}{
				"touser":  touser,
				"toparty": toparty,
				"totag":   totag,
				"msgtype": "text",
				"agentid": h.agentId,
				"text": map[string]string{
					"content": content,
				},
			}
			bodyText, err_ := sonic.MarshalString(body)
			if err_ != nil {
				log.Error("wework marshal req fail", zap.String("content", content), zap.Error(err_))
				continue
			}
			rsp := h.request("POST", url, bodyText)
			if rsp.Status != 200 {
				log.Error("wework send msg net fail", zap.String("content", content), zap.Error(err_))
				continue
			}
			var res = make(map[string]interface{})
			err_ = sonic.UnmarshalString(rsp.Content, &res)
			if err_ != nil {
				log.Error("wework decode rsp fail", zap.String("body", rsp.Content), zap.Error(err_))
				continue
			}
			errCode := utils.GetMapVal(res, "errcode", 0)
			if errCode > 0 {
				log.Error("wework send msg fail", zap.String("content", content),
					zap.String("body", rsp.Content))
				continue
			}
			sentNum += 1
		}
		return sentNum
	}
}
