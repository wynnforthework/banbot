package rpc

import (
	"fmt"
	"github.com/sasha-s/go-deadlock"

	"github.com/banbox/banbot/btime"
	"github.com/banbox/banexg/log"
	"github.com/banbox/banexg/utils"
	"go.uber.org/zap"
)

/**
Get AccessToken
https://developer.work.weixin.qq.com/document/path/91039
Enterprise WeChat push message. 企业微信推送消息
https://developer.work.weixin.qq.com/document/path/90236
*/

type WeCred struct {
	corpId      string
	corpSecret  string
	accessToken string
	expireAt    int64
	lock        *deadlock.Mutex
}

type WeWork struct {
	*WebHook
	cred    *WeCred
	agentId string
	toUser  string
	toParty string
	toTag   string
}

const (
	urlBase = "https://qyapi.weixin.qq.com"
)

var (
	creds = make(map[string]*WeCred)
)

func NewWeWork(name string, item map[string]interface{}) *WeWork {
	hook := NewWebHook(name, item)
	res := &WeWork{
		WebHook: hook,
		cred:    getWeCred(item),
		agentId: utils.GetMapVal(item, "agent_id", ""),
		toUser:  utils.GetMapVal(item, "touser", ""),
		toParty: utils.GetMapVal(item, "toparty", ""),
		toTag:   utils.GetMapVal(item, "totag", ""),
	}
	if !res.cred.valid() || res.agentId == "" {
		panic(name + ": `corp_id`, `corp_secret`, `agent_id` is required")
	}
	res.doSendMsgs = makeDoSendMsg(res)
	return res
}

func getWeCred(item map[string]interface{}) *WeCred {
	itemKey := utils.GetMapVal(item, "agent_id", "")
	cred, ok := creds[itemKey]
	if !ok {
		cred = &WeCred{
			corpId:     utils.GetMapVal(item, "corp_id", ""),
			corpSecret: utils.GetMapVal(item, "corp_secret", ""),
			lock:       &deadlock.Mutex{},
		}
		creds[itemKey] = cred
	}
	return cred
}

type WeWorkRes struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type WeWorkATRes struct {
	WeWorkRes
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (w *WeCred) valid() bool {
	return w.corpId != "" && w.corpSecret != ""
}

func (w *WeCred) getToken() string {
	w.lock.Lock()
	defer w.lock.Unlock()
	curTime := btime.TimeMS()
	if w.expireAt > curTime {
		// The accessToken here may be valid or empty. If it is empty, it means that the last acquisition failed and the current state is in the waiting and prohibited retry phase.
		// 这里accessToken可能有效，也可能为空。为空表示上次获取失败，当前处于等待禁止重试阶段
		return w.accessToken
	}
	// Only one token request retry is allowed per minute.
	// 1分钟仅允许重试一次请求token
	w.expireAt = curTime + 60000
	name := w.corpId
	url := fmt.Sprintf("%s/cgi-bin/gettoken?corpid=%s&corpsecret=%s", urlBase, w.corpId, w.corpSecret)
	rsp := request("GET", url, "")
	if rsp.Error != nil {
		log.Error("wework get token fail", zap.String("name", name), zap.Error(rsp.Error))
		return ""
	}
	var res WeWorkATRes
	err_ := utils.UnmarshalString(rsp.Content, &res, utils.JsonNumDefault)
	if err_ != nil {
		log.Error("wework parse rsp fail", zap.String("name", name), zap.Error(rsp.Error),
			zap.String("body", rsp.Content))
		return ""
	} else if res.ErrCode != 0 {
		log.Error("wework get token fail", zap.String("name", name), zap.String("rsp", rsp.Content))
	}
	w.accessToken = res.AccessToken
	if res.ExpiresIn > 0 {
		w.expireAt = curTime + int64(res.ExpiresIn*1000)
	}
	return w.accessToken
}

type WeWorkSendRes struct {
	WeWorkRes
	InvalidUser    string `json:"invaliduser"`
	InvalidParty   string `json:"invalidparty"`
	InvalidTag     string `json:"invalidtag"`
	UnlicensedUser string `json:"unlicenseduser"`
	Msgid          string `json:"msgid"`
	ResponseCode   string `json:"response_code"`
}

func makeDoSendMsg(h *WeWork) func([]map[string]string) []map[string]string {
	return func(msgList []map[string]string) []map[string]string {
		token := h.cred.getToken()
		if token == "" {
			return msgList
		}
		url := fmt.Sprintf("%s/cgi-bin/message/send?access_token=%s", urlBase, token)
		fails := []map[string]string{}
		for _, msg := range msgList {
			content, _ := msg["content"]
			if content == "" {
				log.Error("wework get empty msg, skip")
				continue
			}
			var body = map[string]interface{}{
				"touser":  h.toUser,
				"toparty": h.toParty,
				"totag":   h.toTag,
				"msgtype": "text",
				"agentid": h.agentId,
				"text": map[string]string{
					"content": content,
				},
			}
			bodyText, err_ := utils.MarshalString(body)
			if err_ != nil {
				log.Error("wework marshal req fail", zap.String("content", content), zap.Error(err_))
				continue
			}
			rsp := request("POST", url, bodyText)
			if rsp.Status != 200 {
				log.Error("wework send msg net fail", zap.String("content", content),
					zap.String("rsp", rsp.Content), zap.Error(err_))
				fails = append(fails, msg)
				continue
			}
			var res WeWorkSendRes
			err_ = utils.UnmarshalString(rsp.Content, &res, utils.JsonNumDefault)
			if err_ != nil {
				log.Error("wework decode rsp fail", zap.String("body", rsp.Content), zap.Error(err_))
				continue
			}
			if res.ErrCode > 0 {
				log.Warn("wework send msg fail", zap.String("content", content),
					zap.String("body", rsp.Content))
				fails = append(fails, msg)
				continue
			}
		}
		return fails
	}
}
