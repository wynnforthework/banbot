package rpc

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/banbox/banexg/log"
	"github.com/banbox/banexg/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

// Telegram 表示Telegram Bot推送渠道，实现了 IWebHook 接口的核心功能
// 其配置继承自 webHookItem，额外支持 token 和 chat_id 字段
// 示例配置（位于 rpc_channels.* 节点下）：
//
//   [rpc_channels.telegram_notice]
//   type = "telegram"                    # 渠道类型，对应 ChlType
//   token = "BOT_TOKEN"                  # 必填，Telegram Bot Token
//   chat_id = "CHAT_ID"                  # 必填，聊天ID（可以是用户ID或群组ID）
//   proxy = "http://127.0.0.1:7897"      # 可选，代理地址
//   msg_types = ["status", "exception"]
//   retry_delay = 30
//   min_intv_secs = 5
//
// 通过 SendMsg -> Queue -> doSendMsgs 的链路实现异步批量发送与失败重试
// 实际发送调用 Telegram Bot API 的 sendMessage 接口。

type Telegram struct {
	*WebHook
	token   string
	chatId  int64
	proxy   string
	bot     *bot.Bot
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewTelegram 构造函数，基于通用 WebHook 创建 Telegram 发送实例
func NewTelegram(name string, item map[string]interface{}) *Telegram {
	hook := NewWebHook(name, item)
	
	token := utils.GetMapVal(item, "token", "")
	if token == "" {
		panic(name + ": `token` is required")
	}
	
	chatIdStr := utils.GetMapVal(item, "chat_id", "")
	if chatIdStr == "" {
		panic(name + ": `chat_id` is required")
	}
	
	chatId, err := strconv.ParseInt(chatIdStr, 10, 64)
	if err != nil {
		panic(name + ": invalid `chat_id`, must be a number: " + err.Error())
	}
	
	// 从配置中读取代理设置
	proxy := utils.GetMapVal(item, "proxy", "")
	
	// 创建带代理的HTTP客户端
	httpClient := createProxyClient(proxy)
	
	// 创建bot实例
	ctx, cancel := context.WithCancel(context.Background())
	botInstance, err := bot.New(token, bot.WithHTTPClient(30*time.Second, httpClient))
	if err != nil {
		cancel()
		panic(name + ": failed to create bot: " + err.Error())
	}
	
	res := &Telegram{
		WebHook: hook,
		token:   token,
		chatId:  chatId,
		proxy:   proxy,
		bot:     botInstance,
		ctx:     ctx,
		cancel:  cancel,
	}
	
	res.doSendMsgs = makeDoSendMsgTelegram(res)
	return res
}

// createProxyClient 创建支持代理的HTTP客户端
func createProxyClient(proxyURL string) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	if proxyURL != "" {
		if proxy, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxy)
			log.Info("Using proxy for Telegram", zap.String("proxy", proxyURL))
		} else {
			log.Warn("Invalid proxy URL", zap.String("proxy", proxyURL), zap.Error(err))
		}
	}
	
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// Close 关闭Telegram客户端
func (t *Telegram) Close() {
	if t.cancel != nil {
		t.cancel()
	}
}

// makeDoSendMsgTelegram 返回批量Telegram消息发送函数，符合 WebHook.doSendMsgs 的签名要求
func makeDoSendMsgTelegram(t *Telegram) func([]map[string]string) []map[string]string {
	return func(msgList []map[string]string) []map[string]string {
		var b strings.Builder
		for i, msg := range msgList {
			content, _ := msg["content"]
			if content == "" {
				log.Error("telegram get empty msg, skip")
				continue
			}
			if i > 0 {
				b.WriteString("\n\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
			}
			b.WriteString(content)
		}
		
		if b.Len() == 0 {
			return nil
		}

		text := b.String()
		// Telegram消息长度限制为4096字符
		if len(text) > 4096 {
			text = text[:4093] + "..."
		}

		log.Debug("telegram sending message", zap.String("text", text), zap.Int64("chat_id", t.chatId))
		
		// 使用go-telegram/bot库发送消息
		_, err := t.bot.SendMessage(t.ctx, &bot.SendMessageParams{
			ChatID:    t.chatId,
			Text:      text,
			ParseMode: models.ParseModeHTML,
		})
		
		if err != nil {
			log.Error("telegram send msg fail", zap.String("text", text), 
				zap.Int64("chat_id", t.chatId), zap.Error(err))
			return msgList
		}

		log.Debug("telegram send msg success", zap.Int("count", len(msgList)))
		return nil
	}
}
