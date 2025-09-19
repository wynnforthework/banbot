package rpc

import (
	"sync"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/banbox/banbot/config"
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

var (
	telegramInstances = make(map[string]*Telegram)
	telegramMutex     sync.RWMutex
	// 订单管理接口，由外部注入实现，避免循环依赖
	orderManager OrderManagerInterface
	// 钱包信息提供者，由外部注入，避免循环依赖
	walletProvider WalletInfoProvider
)

// OrderInfo 订单信息结构
type OrderInfo struct {
	ID       int64   `json:"id"`
	Symbol   string  `json:"symbol"`
	Short    bool    `json:"short"`
	Price    float64 `json:"price"`
	Amount   float64 `json:"amount"`
	EnterTag string  `json:"enter_tag"`
	Account  string  `json:"account"`
}

// OrderManagerInterface 订单管理接口，避免循环依赖
type OrderManagerInterface interface {
	GetActiveOrders(account string) ([]*OrderInfo, error)
	CloseOrder(account string, orderID int64) error
	CloseAllOrders(account string) (int, int, error) // success count, failed count, error
	GetOrderStats(account string) (longCount, shortCount int, err error)
}

// SetOrderManager 设置订单管理器（由外部调用）
func SetOrderManager(mgr OrderManagerInterface) {
	orderManager = mgr
}

// WalletInfoProvider 钱包信息提供者接口
type WalletInfoProvider interface {
    // 返回 单账户: 总额(法币), 可用(法币), 未实现盈亏(法币)
    GetSummary(account string) (totalLegal float64, availableLegal float64, unrealizedPOLLegal float64)
}

// SetWalletInfoProvider 设置钱包信息提供者（由外部调用）
func SetWalletInfoProvider(p WalletInfoProvider) {
    walletProvider = p
}

type Telegram struct {
	*WebHook
	token           string
	chatId          int64
	proxy           string
	bot             *bot.Bot
	ctx             context.Context
	cancel          context.CancelFunc
	tradingDisabled map[string]time.Time // account -> disabled until time
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
		WebHook:         hook,
		token:           token,
		chatId:          chatId,
		proxy:           proxy,
		bot:             botInstance,
		ctx:             ctx,
		cancel:          cancel,
		tradingDisabled: make(map[string]time.Time),
	}
	
	res.doSendMsgs = makeDoSendMsgTelegram(res)
	
	// 设置命令处理器
	res.setupCommandHandlers()
	
	// 注册到全局实例管理器
	telegramMutex.Lock()
	telegramInstances[name] = res
	telegramMutex.Unlock()
	
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
	
	// 从全局实例管理器中移除
	telegramMutex.Lock()
	for name, instance := range telegramInstances {
		if instance == t {
			delete(telegramInstances, name)
			break
		}
	}
	telegramMutex.Unlock()
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

// setupCommandHandlers 设置Telegram Bot命令处理器
func (t *Telegram) setupCommandHandlers() {
	// 注册命令处理器
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "/orders", bot.MatchTypeExact, t.handleOrdersCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "/close", bot.MatchTypePrefix, t.handleCloseCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "/status", bot.MatchTypeExact, t.handleStatusCommand)
	// 钱包
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "/wallet", bot.MatchTypeExact, t.handleWalletCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "/disable", bot.MatchTypePrefix, t.handleDisableCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "/enable", bot.MatchTypeExact, t.handleEnableCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "/help", bot.MatchTypeExact, t.handleHelpCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "/menu", bot.MatchTypeExact, t.handleMenuCommand)
	
	// 注册键盘按钮处理器
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "📊 查看订单", bot.MatchTypeExact, t.handleKeyboardOrdersCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "📈 开单状态", bot.MatchTypeExact, t.handleKeyboardStatusCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "👛 查看钱包", bot.MatchTypeExact, t.handleKeyboardWalletCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "🚫 禁止开单", bot.MatchTypeExact, t.handleKeyboardDisableCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "✅ 启用开单", bot.MatchTypeExact, t.handleKeyboardEnableCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "❌ 平仓所有", bot.MatchTypeExact, t.handleKeyboardCloseAllCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "🔄 刷新菜单", bot.MatchTypeExact, t.handleMenuCommand)
	t.bot.RegisterHandler(bot.HandlerTypeMessageText, "❌ 隐藏菜单", bot.MatchTypeExact, t.handleHideMenuCommand)
	
	// 注册内联键盘回调处理器
	t.bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, "", bot.MatchTypePrefix, t.handleCallbackQuery)
	
	// 启动Bot更新监听
	go func() {
		log.Info("Starting Telegram bot command listener", zap.Int64("chat_id", t.chatId))
		defer func() {
			if r := recover(); r != nil {
				log.Error("Telegram bot panic", zap.Any("panic", r))
			}
		}()
		t.bot.Start(t.ctx)
		log.Info("Telegram bot stopped")
	}()
}

// handleOrdersCommand 处理 /orders 命令 - 获取订单列表
func (t *Telegram) handleOrdersCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}
    
    response := t.getOrdersList()
    kb := t.buildOrdersInlineKeyboard()
    
    _, err := b.SendMessage(ctx, &bot.SendMessageParams{
        ChatID:      update.Message.Chat.ID,
        Text:        response,
        ParseMode:   models.ParseModeHTML,
        ReplyMarkup: kb,
    })
    if err != nil {
        log.Error("Failed to send orders message", zap.Error(err))
    }
}

// handleCloseCommand 处理 /close 命令 - 强制平仓订单
func (t *Telegram) handleCloseCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}
	
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		response := "❌ <b>用法错误</b>\n\n" +
			"请使用: <code>/close [订单ID|all]</code>\n\n" +
			"示例:\n" +
			"• <code>/close 123</code> - 平仓指定订单\n" +
			"• <code>/close all</code> - 平仓所有订单"
		t.sendResponse(b, update, response)
		return
	}
	
	orderID := parts[1]
	response := t.closeOrders(orderID)
	t.sendResponse(b, update, response)
}

// handleStatusCommand 处理 /status 命令 - 获取开单状态
func (t *Telegram) handleStatusCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}
	
	response := t.getTradingStatus()
	t.sendResponse(b, update, response)
}

// handleDisableCommand 处理 /disable 命令 - 禁止开单
func (t *Telegram) handleDisableCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}
	
	parts := strings.Fields(update.Message.Text)
	hours := 1 // 默认1小时
	
	if len(parts) >= 2 {
		if h, err := strconv.Atoi(parts[1]); err == nil && h > 0 && h <= 24 {
			hours = h
		}
	}
	
	response := t.disableTrading(hours)
	t.sendResponse(b, update, response)
}

// handleEnableCommand 处理 /enable 命令 - 启用开单
func (t *Telegram) handleEnableCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}
	
	response := t.enableTrading()
	t.sendResponse(b, update, response)
}

// handleHelpCommand 处理 /help 命令 - 显示帮助信息
func (t *Telegram) handleHelpCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}
	
	response := "🤖 <b>BanBot Telegram 命令帮助</b>\n\n" +
		"<b>订单管理:</b>\n" +
		"• <code>/menu</code> - 显示操作菜单（推荐）\n" +
		"• <code>/orders</code> - 查看当前订单列表\n" +
		"• <code>/close [订单ID|all]</code> - 平仓指定订单或所有订单\n\n" +
		"<b>交易控制:</b>\n" +
		"• <code>/status</code> - 查看当前交易状态\n" +
		"• <code>/disable [小时]</code> - 禁止开单(默认1小时)\n" +
		"• <code>/enable</code> - 重新启用开单\n\n" +
		"<b>钱包信息:</b>\n" +
		"• <code>/wallet</code> - 查看钱包总额/可用/未实现盈亏\n\n" +
		"<b>其他:</b>\n" +
		"• <code>/help</code> - 显示此帮助信息\n\n" +
		"💡 <i>提示：使用 /menu 命令可获得更便捷的按钮操作界面</i>\n" +
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	
	t.sendResponse(b, update, response)
}

// handleMenuCommand 处理 /menu 命令 - 显示主菜单
func (t *Telegram) handleMenuCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}

	// 创建 Reply Keyboard（显示在键盘上）
	kb := &models.ReplyKeyboardMarkup{
		Keyboard: [][]models.KeyboardButton{
			{
				{Text: "📊 查看订单"},
				{Text: "📈 开单状态"},
			},
			{
				{Text: "👛 查看钱包"},
				{Text: "❌ 平仓所有"},
			},
			{
				{Text: "🚫 禁止开单"},
				{Text: "✅ 启用开单"},
			},
			{
				{Text: "🔄 刷新菜单"},
			},
			{
				{Text: "❌ 隐藏菜单"},
			},
		},
		ResizeKeyboard:  true,
		OneTimeKeyboard: false,
	}

	menuText := `🎛️ <b>BanBot 操作菜单</b>
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

请选择您要执行的操作：

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      update.Message.Chat.ID,
		Text:        menuText,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if err != nil {
		log.Error("Failed to send menu", zap.Error(err))
	}
}

// handleCallbackQuery 处理内联键盘回调
func (t *Telegram) handleCallbackQuery(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}

	if !t.isAuthorizedCallback(update) {
		return
	}

	data := update.CallbackQuery.Data
	
	// 先回应回调查询
	_, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
		Text:           "处理中...",
	})
	if err != nil {
		log.Error("Failed to answer callback query", zap.Error(err))
	}

	// 处理不同的回调数据
	switch data {
	case "action:orders":
		t.handleOrdersCallback(ctx, b, update)
	case "action:status":
		t.handleStatusCallback(ctx, b, update)
	case "action:disable":
		t.handleDisableCallback(ctx, b, update)
	case "action:enable":
		t.handleEnableCallback(ctx, b, update)
	case "action:wallet":
		t.handleWalletCallback(ctx, b, update)
	case "action:close_all":
		t.handleCloseAllCallback(ctx, b, update)
	case "action:refresh":
		t.handleMenuCallback(ctx, b, update)
	default:
		if strings.HasPrefix(data, "close:") {
			t.handleCloseOrderCallback(ctx, b, update, data)
		}
	}
}

// isAuthorizedCallback 检查回调查询用户是否有权限
func (t *Telegram) isAuthorizedCallback(update *models.Update) bool {
	if update.CallbackQuery == nil {
		return false
	}
	
	userID := update.CallbackQuery.From.ID
	return userID == t.chatId
}

// isAuthorized 检查用户是否有权限使用命令
func (t *Telegram) isAuthorized(update *models.Update) bool {
	if update.Message == nil || update.Message.From == nil {
		return false
	}
	
	// 检查是否是配置的chat_id
	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID
	
	// 如果是私聊，检查用户ID；如果是群聊，检查群ID
	if chatID == t.chatId || userID == t.chatId {
		return true
	}
	
	log.Warn("Unauthorized telegram command attempt", 
		zap.Int64("user_id", userID), 
		zap.Int64("chat_id", chatID),
		zap.Int64("authorized_chat_id", t.chatId))
	return false
}

// sendResponse 发送响应消息
func (t *Telegram) sendResponse(b *bot.Bot, update *models.Update, response string) {
	_, err := b.SendMessage(t.ctx, &bot.SendMessageParams{
		ChatID:    update.Message.Chat.ID,
		Text:      response,
		ParseMode: models.ParseModeHTML,
	})
	
	if err != nil {
		log.Error("Failed to send telegram response", zap.Error(err))
	}
}

// getOrdersList 获取订单列表
func (t *Telegram) getOrdersList() string {
	var response strings.Builder
	response.WriteString("📊 <b>当前订单列表</b>\n")
	response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
	
	if orderManager == nil {
		response.WriteString("❌ 订单管理器未初始化\n")
		response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		return response.String()
	}
	
	totalOrders := 0
	
	// 遍历所有账户
	for account := range config.Accounts {
		orders, err := orderManager.GetActiveOrders(account)
		if err != nil {
			log.Error("Failed to get orders", zap.String("account", account), zap.Error(err))
			continue
		}
		
		if len(orders) == 0 {
			continue
		}
		
		response.WriteString(fmt.Sprintf("🏷️ <b>账户:</b> <code>%s</code>\n", account))
		
		for _, order := range orders {
			totalOrders++
			
			// 订单方向
			direction := "📈 多"
			if order.Short {
				direction = "📉 空"
			}
			
			// 格式化订单信息
			response.WriteString(fmt.Sprintf(
				"• <code>%d</code> %s <code>%s</code>\n"+
				"  💰 价格: <code>%.5f</code> | 数量: <code>%.4f</code>\n"+
				"  📊 盈亏: <code>计算中...</code> | 标签: <code>%s</code>\n\n",
				order.ID,
				direction,
				order.Symbol,
				order.Price,
				order.Amount,
				order.EnterTag,
			))
		}
	}
	
	if totalOrders == 0 {
		response.WriteString("暂无活跃订单\n")
	} else {
		response.WriteString(fmt.Sprintf("总计: <b>%d</b> 个活跃订单", totalOrders))
	}
	
	response.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	
	return response.String()
}

// buildOrdersInlineKeyboard 构建订单列表对应的内联键盘（每单平仓 + 批量操作）
func (t *Telegram) buildOrdersInlineKeyboard() *models.InlineKeyboardMarkup {
    var rows [][]models.InlineKeyboardButton
    if orderManager != nil {
        orders, err := orderManager.GetActiveOrders("default")
        if err == nil && len(orders) > 0 {
            for _, od := range orders {
                rows = append(rows, []models.InlineKeyboardButton{{
                    Text:        fmt.Sprintf("❌ 平仓 %d", od.ID),
                    CallbackData: fmt.Sprintf("close:%d", od.ID),
                }})
            }
            rows = append(rows, []models.InlineKeyboardButton{
                {Text: "❌ 平仓所有订单", CallbackData: "action:close_all"},
                {Text: "🔄 刷新订单", CallbackData: "action:orders"},
            })
        } else {
            rows = append(rows, []models.InlineKeyboardButton{
                {Text: "🔄 刷新订单", CallbackData: "action:orders"},
            })
        }
    }
    return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// closeOrders 平仓订单
func (t *Telegram) closeOrders(orderID string) string {
	if orderID == "all" {
		return t.closeAllOrders()
	}
	
	if orderManager == nil {
		return "❌ <b>错误</b>: 订单管理器未初始化"
	}
	
	// 解析订单ID
	id, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return "❌ <b>错误</b>: 无效的订单ID"
	}
	
	// 先尝试默认账户
	defaultAccount := "default"
	err = orderManager.CloseOrder(defaultAccount, id)
	if err == nil {
		return fmt.Sprintf("✅ <b>平仓成功</b>\n\n📊 订单ID: <code>%d</code>\n🎯 账户: <code>%s</code>\n⏰ 时间: %s\n\n已提交平仓请求，请等待执行完成。", 
			id, defaultAccount, time.Now().Format("15:04:05"))
	}
	
	// 如果默认账户中没有找到，再查找其他账户
	for account := range config.Accounts {
		if account == defaultAccount {
			continue // 跳过已经尝试过的默认账户
		}
		err := orderManager.CloseOrder(account, id)
		if err == nil {
			return fmt.Sprintf("✅ <b>平仓成功</b>\n\n📊 订单ID: <code>%d</code>\n🎯 账户: <code>%s</code>\n⏰ 时间: %s\n\n已提交平仓请求，请等待执行完成。", 
				id, account, time.Now().Format("15:04:05"))
		}
	}
	
	return fmt.Sprintf("❌ <b>订单未找到</b>\n\n📊 订单ID: <code>%d</code>\n⏰ 时间: %s\n\n请检查订单ID是否正确，或使用 <code>/orders</code> 查看当前活跃订单。", 
		id, time.Now().Format("15:04:05"))
}

// closeAllOrders 平仓所有订单
func (t *Telegram) closeAllOrders() string {
	var response strings.Builder
	response.WriteString("🔄 <b>批量平仓结果</b>\n")
	response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
	
	if orderManager == nil {
		response.WriteString("❌ 订单管理器未初始化\n")
		response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		return response.String()
	}
	
	totalClosed := 0
	totalFailed := 0
	
	for account := range config.Accounts {
		response.WriteString(fmt.Sprintf("🏷️ <b>账户:</b> <code>%s</code>\n", account))
		
		successCount, failedCount, err := orderManager.CloseAllOrders(account)
		if err != nil {
			response.WriteString(fmt.Sprintf("  ❌ 获取订单失败: %s\n", err.Error()))
			continue
		}
		
		totalClosed += successCount
		totalFailed += failedCount
		
		response.WriteString(fmt.Sprintf("  ✅ 成功: %d | ❌ 失败: %d\n", successCount, failedCount))
		response.WriteString("\n")
	}
	
	response.WriteString(fmt.Sprintf("📊 <b>统计:</b> 成功 %d | 失败 %d", totalClosed, totalFailed))
	response.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	
	return response.String()
}

// getOrdersListWithKeyboard 获取订单列表并返回是否有订单的标志
func (t *Telegram) getOrdersListWithKeyboard(account string) (string, bool) {
	var response strings.Builder
	response.WriteString("📊 <b>活跃订单列表</b>\n")
	response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
	
	if orderManager == nil {
		response.WriteString("❌ 订单管理器未初始化\n")
		response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		return response.String(), false
	}
	
	hasOrders := false
	totalOrders := 0
	
	// 获取指定账户的订单
	orders, err := orderManager.GetActiveOrders(account)
	if err != nil {
		log.Error("Failed to get orders", zap.String("account", account), zap.Error(err))
		response.WriteString(fmt.Sprintf("❌ 获取账户 %s 订单失败: %v\n", account, err))
	} else if len(orders) > 0 {
		hasOrders = true
		response.WriteString(fmt.Sprintf("🏷️ <b>账户:</b> %s\n\n", account))
		
		for _, order := range orders {
			totalOrders++
			direction := "📈 多单"
			if order.Short {
				direction = "📉 空单"
			}
			
			response.WriteString(fmt.Sprintf("• <code>%d</code> %s <code>%s</code>\n", order.ID, direction, order.Symbol))
			response.WriteString(fmt.Sprintf("  💰 价格: <code>%.5f</code> | 数量: <code>%.4f</code>\n", order.Price, order.Amount))
			if order.EnterTag != "" {
				response.WriteString(fmt.Sprintf("  🏷️ 标签: <code>%s</code>\n", order.EnterTag))
			}
			response.WriteString("\n")
		}
	}
	
	if totalOrders == 0 {
		response.WriteString("📭 <b>暂无活跃订单</b>\n")
		response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	} else {
		response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		response.WriteString(fmt.Sprintf("📊 <b>总计:</b> %d 个活跃订单", totalOrders))
	}
	
	return response.String(), hasOrders
}

// getTradingStatus 获取交易状态
func (t *Telegram) getTradingStatus() string {
	var response strings.Builder
	response.WriteString("📊 <b>交易状态</b>\n")
	response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
	
	now := time.Now()
	
	for account := range config.Accounts {
		response.WriteString(fmt.Sprintf("🏷️ <b>账户:</b> <code>%s</code>\n", account))
		
		// 检查是否被禁用
		if disabledUntil, exists := t.tradingDisabled[account]; exists && now.Before(disabledUntil) {
			remaining := disabledUntil.Sub(now)
			response.WriteString(fmt.Sprintf("  🚫 <b>状态:</b> 开单已禁用\n"))
			response.WriteString(fmt.Sprintf("  ⏰ <b>剩余:</b> %s\n", formatDuration(remaining)))
		} else {
			response.WriteString("  ✅ <b>状态:</b> 开单正常\n")
		}
		
		// 获取当前订单数量
		if orderManager != nil {
			longCount, shortCount, err := orderManager.GetOrderStats(account)
			if err == nil {
				response.WriteString(fmt.Sprintf("  📈 <b>多单:</b> %d | 📉 <b>空单:</b> %d\n", longCount, shortCount))
			}
		}
		
		response.WriteString("\n")
	}
	
	response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	
	return response.String()
}

// disableTrading 禁用交易
func (t *Telegram) disableTrading(hours int) string {
	disabledUntil := time.Now().Add(time.Duration(hours) * time.Hour)
	
	// 对所有账户禁用交易
	for account := range config.Accounts {
		t.tradingDisabled[account] = disabledUntil
	}
	
	return fmt.Sprintf(
		"🚫 <b>开单已禁用</b>\n\n"+
		"⏰ <b>禁用时长:</b> %d 小时\n"+
		"📅 <b>恢复时间:</b> %s\n\n"+
		"使用 <code>/enable</code> 可提前恢复开单",
		hours,
		disabledUntil.Format("2006-01-02 15:04:05"),
	)
}

// enableTrading 启用交易
func (t *Telegram) enableTrading() string {
	// 清除所有账户的禁用状态
	t.tradingDisabled = make(map[string]time.Time)
	
	return "✅ <b>开单已恢复</b>\n\n所有账户的交易功能已重新启用"
}

// IsTradingDisabled 检查指定账户是否被禁用交易（供外部调用）
func (t *Telegram) IsTradingDisabled(account string) bool {
	if disabledUntil, exists := t.tradingDisabled[account]; exists {
		return time.Now().Before(disabledUntil)
	}
	return false
}

// formatDuration 格式化时间间隔
func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	
	if hours > 0 {
		return fmt.Sprintf("%d小时%d分钟", hours, minutes)
	}
	return fmt.Sprintf("%d分钟", minutes)
}

// IsTradingDisabledByTelegram 检查指定账户是否被Telegram Bot禁用交易（全局函数）
func IsTradingDisabledByTelegram(account string) bool {
	telegramMutex.RLock()
	defer telegramMutex.RUnlock()
	
	// 检查所有Telegram实例
	for _, instance := range telegramInstances {
		if instance.IsTradingDisabled(account) {
			return true
		}
	}
	return false
}

// initTradingDisableHook 初始化交易禁用钩子
func initTradingDisableHook() {
	// 需要通过反射或接口方式设置，避免循环依赖
	// 这个函数将在适当的时候被调用
}

// handleOrdersCallback 处理查看订单回调
func (t *Telegram) handleOrdersCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	ordersList, hasOrders := t.getOrdersListWithKeyboard("default")

	// 创建键盘，包含每个订单的单独平仓按钮
	var rows [][]models.InlineKeyboardButton
	if hasOrders {
		// 从文本里解析订单ID太复杂，这里直接重新获取订单构建按钮
		if orderManager != nil {
			orders, err := orderManager.GetActiveOrders("default")
			if err == nil {
				for _, od := range orders {
					btn := models.InlineKeyboardButton{Text: fmt.Sprintf("❌ 平仓 %d", od.ID), CallbackData: fmt.Sprintf("close:%d", od.ID)}
					rows = append(rows, []models.InlineKeyboardButton{btn})
				}
			}
		}
		// 追加操作行
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "❌ 平仓所有订单", CallbackData: "action:close_all"},
			{Text: "🔄 刷新订单", CallbackData: "action:orders"},
		})
		rows = append(rows, []models.InlineKeyboardButton{{Text: "🔙 返回菜单", CallbackData: "action:refresh"}})
	} else {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "🔄 刷新订单", CallbackData: "action:orders"},
			{Text: "🔙 返回菜单", CallbackData: "action:refresh"},
		})
	}
	kb := &models.InlineKeyboardMarkup{InlineKeyboard: rows}

	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      update.CallbackQuery.Message.Message.Chat.ID,
		MessageID:   update.CallbackQuery.Message.Message.ID,
		Text:        ordersList,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if err != nil {
		log.Error("Failed to edit message with orders", zap.Error(err))
	}
}

// handleStatusCallback 处理查看状态回调
func (t *Telegram) handleStatusCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	status := t.getTradingStatus()
	
	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "🔄 刷新状态", CallbackData: "action:status"},
				{Text: "🔙 返回菜单", CallbackData: "action:refresh"},
			},
		},
	}

	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      update.CallbackQuery.Message.Message.Chat.ID,
		MessageID:   update.CallbackQuery.Message.Message.ID,
		Text:        status,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if err != nil {
		log.Error("Failed to edit message with status", zap.Error(err))
	}
}

// handleDisableCallback 处理禁止开单回调
func (t *Telegram) handleDisableCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	// 默认禁用1小时
	duration := time.Hour
	t.disableTrading(1) // 1 hour
	
	response := fmt.Sprintf("🚫 <b>交易已禁用</b>\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n"+
		"⏰ <b>禁用时长:</b> %s\n"+
		"🕒 <b>恢复时间:</b> %s\n\n"+
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━",
		"1小时", time.Now().Add(duration).Format("2006-01-02 15:04:05"))

	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "✅ 立即启用", CallbackData: "action:enable"},
				{Text: "🔙 返回菜单", CallbackData: "action:refresh"},
			},
		},
	}

	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      update.CallbackQuery.Message.Message.Chat.ID,
		MessageID:   update.CallbackQuery.Message.Message.ID,
		Text:        response,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if err != nil {
		log.Error("Failed to edit message with disable status", zap.Error(err))
	}
}

// handleEnableCallback 处理启用开单回调
func (t *Telegram) handleEnableCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	t.enableTrading()
	
	response := "✅ <b>交易已启用</b>\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n" +
		"🎯 <b>状态:</b> 交易功能已恢复正常\n" +
		"⏰ <b>时间:</b> " + time.Now().Format("2006-01-02 15:04:05") + "\n\n" +
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "🚫 禁用交易", CallbackData: "action:disable"},
				{Text: "🔙 返回菜单", CallbackData: "action:refresh"},
			},
		},
	}

	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      update.CallbackQuery.Message.Message.Chat.ID,
		MessageID:   update.CallbackQuery.Message.Message.ID,
		Text:        response,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if err != nil {
		log.Error("Failed to edit message with enable status", zap.Error(err))
	}
}

// handleCloseAllCallback 处理平仓所有订单回调
func (t *Telegram) handleCloseAllCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	result := t.closeAllOrders()
	
	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "📊 查看订单", CallbackData: "action:orders"},
				{Text: "🔙 返回菜单", CallbackData: "action:refresh"},
			},
		},
	}
	
	// FIXME: Replace this placeholder with proper keyboard

	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      update.CallbackQuery.Message.Message.Chat.ID,
		MessageID:   update.CallbackQuery.Message.Message.ID,
		Text:        result,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if err != nil {
		log.Error("Failed to edit message with close all result", zap.Error(err))
	}
}

// handleMenuCallback 处理返回菜单回调
func (t *Telegram) handleMenuCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "📊 查看订单", CallbackData: "action:orders"},
				{Text: "📈 开单状态", CallbackData: "action:status"},
			},
			{
				{Text: "👛 查看钱包", CallbackData: "action:wallet"},
				{Text: "❌ 平仓所有", CallbackData: "action:close_all"},
			},
			{
				{Text: "🚫 禁用交易", CallbackData: "action:disable"},
				{Text: "✅ 启用开单", CallbackData: "action:enable"},
			},
		},
	}
	

	menuText := `🎛️ <b>BanBot 操作菜单</b>
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

请选择您要执行的操作：

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`

	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      update.CallbackQuery.Message.Message.Chat.ID,
		MessageID:   update.CallbackQuery.Message.Message.ID,
		Text:        menuText,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if err != nil {
		log.Error("Failed to edit message with menu", zap.Error(err))
	}
}

// handleCloseOrderCallback 处理平仓特定订单回调
func (t *Telegram) handleCloseOrderCallback(ctx context.Context, b *bot.Bot, update *models.Update, data string) {
	// 解析订单ID：close:12345
	parts := strings.Split(data, ":")
	if len(parts) != 2 {
		return
	}
	
	orderIDStr := parts[1]
	
	result := t.closeOrders(orderIDStr)
	
	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "📊 查看订单", CallbackData: "action:orders"},
				{Text: "🔙 返回菜单", CallbackData: "action:refresh"},
			},
		},
	}
	
	// FIXME: Replace this placeholder with proper keyboard

	_, editErr := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      update.CallbackQuery.Message.Message.Chat.ID,
		MessageID:   update.CallbackQuery.Message.Message.ID,
		Text:        result,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if editErr != nil {
		log.Error("Failed to edit message with close order result", zap.Error(editErr))
	}
}

// 键盘按钮处理函数

// handleKeyboardOrdersCommand 处理键盘"查看订单"按钮
func (t *Telegram) handleKeyboardOrdersCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}

	ordersList, hasOrders := t.getOrdersListWithKeyboard("default")

	var rows [][]models.InlineKeyboardButton
	if hasOrders && orderManager != nil {
		orders, err := orderManager.GetActiveOrders("default")
		if err == nil {
			for _, od := range orders {
				btn := models.InlineKeyboardButton{Text: fmt.Sprintf("❌ 平仓 %d", od.ID), CallbackData: fmt.Sprintf("close:%d", od.ID)}
				rows = append(rows, []models.InlineKeyboardButton{btn})
			}
		}
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "❌ 平仓所有订单", CallbackData: "action:close_all"},
			{Text: "🔄 刷新订单", CallbackData: "action:orders"},
		})
		rows = append(rows, []models.InlineKeyboardButton{{Text: "🔙 返回菜单", CallbackData: "action:refresh"}})
	}

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      update.Message.Chat.ID,
		Text:        ordersList,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
	if err != nil {
		log.Error("Failed to send orders with inline buttons", zap.Error(err))
	}
}

// handleKeyboardStatusCommand 处理键盘"开单状态"按钮
func (t *Telegram) handleKeyboardStatusCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}
	
	status := t.getTradingStatus()
	t.sendResponse(b, update, status)
}

// handleKeyboardDisableCommand 处理键盘"禁止开单"按钮
func (t *Telegram) handleKeyboardDisableCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}
	
	response := t.disableTrading(1) // 默认禁用1小时
	t.sendResponse(b, update, response)
}

// handleKeyboardEnableCommand 处理键盘"启用开单"按钮
func (t *Telegram) handleKeyboardEnableCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}
	
	response := t.enableTrading()
	t.sendResponse(b, update, response)
}

// handleKeyboardCloseAllCommand 处理键盘"平仓所有"按钮
func (t *Telegram) handleKeyboardCloseAllCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}
	
	response := t.closeAllOrders()
	t.sendResponse(b, update, response)
}

// handleHideMenuCommand 处理"隐藏菜单"按钮
func (t *Telegram) handleHideMenuCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}
	
	// 发送隐藏键盘的消息
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "🔄 <b>菜单已隐藏</b>\n\n使用 <code>/menu</code> 命令可以重新显示菜单。",
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: &models.ReplyKeyboardRemove{
			RemoveKeyboard: true,
		},
	})
	if err != nil {
		log.Error("Failed to hide menu", zap.Error(err))
	}
}

// handleWalletCommand 处理 /wallet 命令 - 显示钱包信息
func (t *Telegram) handleWalletCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}

	response := t.getWalletSummary()
	t.sendResponse(b, update, response)
}

// handleKeyboardWalletCommand 处理键盘"查看钱包"按钮
func (t *Telegram) handleKeyboardWalletCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !t.isAuthorized(update) {
		return
	}

	response := t.getWalletSummary()
	// 带内联按钮：刷新与返回
	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "🔄 刷新钱包", CallbackData: "action:wallet"},
				{Text: "🔙 返回菜单", CallbackData: "action:refresh"},
			},
		},
	}

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      update.Message.Chat.ID,
		Text:        response,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if err != nil {
		log.Error("Failed to send wallet summary", zap.Error(err))
	}
}

// handleWalletCallback 处理查看钱包回调
func (t *Telegram) handleWalletCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	response := t.getWalletSummary()
	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "🔄 刷新钱包", CallbackData: "action:wallet"},
				{Text: "🔙 返回菜单", CallbackData: "action:refresh"},
			},
		},
	}

	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      update.CallbackQuery.Message.Message.Chat.ID,
		MessageID:   update.CallbackQuery.Message.Message.ID,
		Text:        response,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if err != nil {
		log.Error("Failed to edit message with wallet summary", zap.Error(err))
	}
}

// getWalletSummary 获取钱包汇总信息
func (t *Telegram) getWalletSummary() string {
	var bld strings.Builder
	bld.WriteString("👛 <b>钱包汇总</b>\n")
	bld.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	totalAll := 0.0
	avaAll := 0.0
	upolAll := 0.0

	for account := range config.Accounts {
		var total, ava, upol float64
		if walletProvider != nil {
			total, ava, upol = walletProvider.GetSummary(account)
		} else {
			bld.WriteString(fmt.Sprintf("🏷️ <b>账户:</b> <code>%s</code>\n", account))
			bld.WriteString("  ❌ 钱包提供者未初始化\n\n")
			continue
		}

		totalAll += total
		avaAll += ava
		upolAll += upol

		bld.WriteString(fmt.Sprintf("🏷️ <b>账户:</b> <code>%s</code>\n", account))
		bld.WriteString(fmt.Sprintf("  💼 <b>总额:</b> <code>%.2f</code>\n", total))
		bld.WriteString(fmt.Sprintf("  💰 <b>可用:</b> <code>%.2f</code>\n", ava))
		bld.WriteString(fmt.Sprintf("  📊 <b>未实现盈亏:</b> <code>%.2f</code>\n\n", upol))
	}

	bld.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	bld.WriteString("📈 <b>合计</b>\n")
	bld.WriteString(fmt.Sprintf("  💼 <b>总额:</b> <code>%.2f</code>\n", totalAll))
	bld.WriteString(fmt.Sprintf("  💰 <b>可用:</b> <code>%.2f</code>\n", avaAll))
	bld.WriteString(fmt.Sprintf("  📊 <b>未实现盈亏:</b> <code>%.2f</code>\n", upolAll))
	bld.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return bld.String()
}

// addCloseButtonsToOrdersList 为订单列表添加单独平仓按钮
func (t *Telegram) addCloseButtonsToOrdersList(account string) string {
	var response strings.Builder
	response.WriteString("📊 <b>活跃订单列表</b>\n")
	response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
	
	if orderManager == nil {
		response.WriteString("❌ 订单管理器未初始化\n")
		response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		return response.String()
	}
	
	totalOrders := 0
	
	// 获取指定账户的订单
	orders, err := orderManager.GetActiveOrders(account)
	if err != nil {
		log.Error("Failed to get orders", zap.String("account", account), zap.Error(err))
		response.WriteString(fmt.Sprintf("❌ 获取账户 %s 订单失败: %v\n", account, err))
	} else if len(orders) > 0 {
		response.WriteString(fmt.Sprintf("🏷️ <b>账户:</b> %s\n\n", account))
		
		for _, order := range orders {
			totalOrders++
			direction := "📈 多单"
			if order.Short {
				direction = "📉 空单"
			}
			
			response.WriteString(fmt.Sprintf("• <code>%d</code> %s <code>%s</code>\n", order.ID, direction, order.Symbol))
			response.WriteString(fmt.Sprintf("  💰 价格: <code>%.5f</code> | 数量: <code>%.4f</code>\n", order.Price, order.Amount))
			if order.EnterTag != "" {
				response.WriteString(fmt.Sprintf("  🏷️ 标签: <code>%s</code>\n", order.EnterTag))
			}
			response.WriteString(fmt.Sprintf("  💡 平仓命令: <code>/close %d</code>\n\n", order.ID))
		}
	}
	
	if totalOrders == 0 {
		response.WriteString("📭 <b>暂无活跃订单</b>\n")
		response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	} else {
		response.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		response.WriteString(fmt.Sprintf("📊 <b>总计:</b> %d 个活跃订单\n", totalOrders))
		response.WriteString("💡 <b>提示:</b> 点击上方平仓命令或直接输入 <code>/close [订单ID]</code> 来平仓指定订单")
	}
	
	return response.String()
}
