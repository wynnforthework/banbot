# Telegram Bot 通知使用指南

## 功能特点

- ✅ 完全兼容现有的 IWebHook 接口
- ✅ 支持批量消息发送（自动合并多条消息）
- ✅ 支持消息重试机制
- ✅ 支持消息类型过滤（status、exception、entry、exit等）
- ✅ 支持账户级别的通知配置
- ✅ 支持关键词过滤
- ✅ 支持HTML格式消息
- ✅ 自动处理Telegram消息长度限制（4096字符）

## 快速配置

### 1. 在配置文件中添加 Telegram 通知

```yaml
rpc_channels:
  telegram_notify:
    type: telegram
    token: "你的Bot Token"
    chat_id: "你的Chat ID"
    msg_types: ["status", "exception", "entry", "exit"]
    min_intv_secs: 5
    retry_delay: 30
```

### 2. 获取必要参数

#### Bot Token:
1. 在Telegram中找到 @BotFather
2. 发送 `/newbot` 创建机器人
3. 按提示设置名称，获得Token

#### Chat ID:
1. 将机器人添加为好友或加入群组
2. 发送任意消息
3. 访问: `https://api.telegram.org/bot<TOKEN>/getUpdates`
4. 在返回JSON中找到 `chat.id`

### 3. 消息类型说明

- `status`: 机器人状态消息（启动、停止等）
- `exception`: 异常错误消息
- `entry`: 开仓通知
- `exit`: 平仓通知
- `market`: 市场相关消息
- `startup`: 启动消息

## 高级配置

### 多账户配置
```yaml
accounts:
  user1:
    rpc_channels:
      - name: telegram_notify
        msg_types: ["entry", "exit"]  # 仅发送交易相关消息
  
  user2:
    rpc_channels:
      - name: telegram_admin
        msg_types: ["exception"]      # 仅发送异常消息
```

### 关键词过滤
```yaml
rpc_channels:
  telegram_critical:
    type: telegram
    token: "TOKEN"
    chat_id: "CHAT_ID"
    keywords: ["ERROR", "CRITICAL", "FAIL"]  # 仅发送包含这些关键词的消息
```

## 消息格式示例

### 开仓通知
```
BanBot/user1 entry
Symbol: BTC/USDT:USDT 1m
Tag: strategy005  long_signal
Price: 45234.56789
Cost: 1000.00
```

### 异常通知
```
BanBot/user1: Connection lost to exchange
```

## 故障排除

### 1. 机器人无法发送消息
- 检查Token是否正确
- 确保机器人已被添加到聊天中
- 验证Chat ID格式（群组ID通常为负数）

### 2. 消息发送失败
- 检查网络连接
- 查看日志中的错误信息
- 验证Telegram API限制（每秒30条消息）

### 3. 消息被截断
- 系统自动处理长消息（>4096字符）
- 超长消息会被截断并添加"..."标识

## 与现有通知方式对比

| 特性 | 企业微信 | 邮件 | Telegram |
|------|----------|------|----------|
| 实时性 | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| 配置难度 | 中等 | 简单 | 简单 |
| 消息格式 | 文本 | 富文本 | HTML/Markdown |
| 免费额度 | 有限 | 依赖服务商 | 免费 |
| 全球可用性 | 限制 | 全球 | 全球 |

## 注意事项

1. Telegram Bot API有速率限制，系统已内置重试机制
2. 群组消息需要机器人有发送权限
3. 私聊需要用户先主动联系机器人
4. 建议为不同重要级别的消息配置不同的聊天群组
