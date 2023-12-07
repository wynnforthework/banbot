package config

import (
	"errors"
	"sync"
)

const (
	MinStakeAmount  = 10   // 最小开单金额
	MaxFetchNum     = 1000 // 单次请求交易所最大返回K线数量
	MaxDownParallel = 5    // 最大同时下载K线任务数
	DefaultDateFmt  = "2006-01-02 15:04:05"
)

const (
	RunModeProd     = "prod"
	RunModeDryRun   = "dry_run"
	RunModeBackTest = "backtest"
	RunModeOther    = "other"
)

const (
	RunEnvProd = "prod"
	RunEnvTest = "test"
)

const (
	BotStateRunning = 1
	BotStateStopped = 2
)

var (
	Cfg                 Config
	m                   sync.Mutex
	run_mode            string
	ErrExchangeNotFound = errors.New("exchange not found")
	ErrDataDirInvalid   = errors.New("Env ban_data_dir is required")
)

// Config 是根配置结构体
type Config struct {
	Name            string `yaml:"name"`
	Env             string `yaml:"env"`
	Loaded          bool
	Debug           bool
	RunMode         string                            `yaml:"run_mode"`
	Leverage        uint16                            `yaml:"leverage"`
	LimitVolSecs    int                               `yaml:"limit_vol_secs"`
	MarketType      string                            `yaml:"market_type"`
	MaxMarketRate   float64                           `yaml:"max_market_rate"`
	OdBookTtl       uint16                            `yaml:"odbook_ttl"`
	OrderType       string                            `yaml:"order_type"`
	PreFire         bool                              `yaml:"prefire"`
	MarginAddRate   float64                           `yaml:"margin_add_rate"`
	ChargeOnBomb    bool                              `yaml:"charge_on_bomb"`
	AutoEditLimit   bool                              `yaml:"auto_edit_limit"`
	TakeOverStgy    string                            `yaml:"take_over_stgy"`
	StakeAmount     float64                           `yaml:"stake_amount"`
	MinOpenRate     float64                           `yaml:"min_open_rate"`
	MaxOpenOrders   uint16                            `yaml:"max_open_orders"`
	WalletAmounts   *WalletAmountsConfig              `yaml:"wallet_amounts"`
	DrawBalanceOver int                               `yaml:"draw_balance_over"`
	StakeCurrency   []string                          `yaml:"stake_currency"`
	FatalStop       *FatalStopConfig                  `yaml:"fatal_stop"`
	FatalStopHours  int                               `yaml:"fatal_stop_hours"`
	TimeRange       string                            `yaml:"timerange"`
	WsStamp         *string                           `yaml:"ws_stamp"`
	RunTimeframes   []string                          `yaml:"run_timeframes"`
	KlineSource     string                            `yaml:"kline_source"`
	WatchJobs       *WatchJobConfig                   `yaml:"watch_jobs"`
	RunPolicy       []*RunPolicyConfig                `yaml:"run_policy"`
	Pairs           []string                          `yaml:"pairs"`
	PairMgr         *PairMgrConfig                    `yaml:"pairmgr"`
	PairFilters     []*CommonPairFilter               `yaml:"pairlists"`
	Exchange        *ExchangeConfig                   `yaml:"exchange"`
	DataDir         string                            `yaml:"data_dir"`
	ExgDataMap      map[string]string                 `yaml:"exg_data_map"`
	Database        *DatabaseConfig                   `yaml:"database"`
	SpiderAddr      string                            `yaml:"spider_addr"`
	APIServer       *APIServerConfig                  `yaml:"api_server"`
	RPCChannels     map[string]map[string]interface{} `yaml:"rpc_channels"`
	Webhook         map[string]map[string]string      `yaml:"webhook"`
}

// WalletAmountsConfig 表示不同货币及其余额的映射
// 键是货币代码，如 "USDT"，值是对应的余额
type WalletAmountsConfig map[string]int

// FatalStopConfig 表示全局止损配置
// 键是时间周期（以分钟为单位），值是对应的损失百分比
type FatalStopConfig map[string]float64

// WatchJobConfig
// K线监听执行的任务，仅用于爬虫端运行
type WatchJobConfig map[string][]string

// 运行的策略，可以多个策略同时运行
type RunPolicyConfig struct {
	Name          string   `yaml:"name"`
	RunTimeframes []string `yaml:"run_Timeframes"`
	MaxPair       int      `yaml:"max_pair"`
}

type DatabaseConfig struct {
	Url       string `yaml:"url"`
	Retention string `yaml:"Retention"`
}

type APIServerConfig struct {
	Enabled         bool     `yaml:"enabled"`           // 是否启用
	ListenIPAddress string   `yaml:"listen_ip_address"` // 绑定地址，0.0.0.0表示暴露到公网
	ListenPort      int      `yaml:"listen_port"`       // 本地监听端口
	Verbosity       string   `yaml:"verbosity"`         // 详细程度
	EnableOpenAPI   bool     `yaml:"enable_openapi"`    // 是否提供所有URL接口文档到"/docs"
	JWTSecretKey    string   `yaml:"jwt_secret_key"`    // 用于密码加密的密钥
	CORSOrigins     []string `yaml:"CORS_origins"`      // banweb访问时，��要这里添加banweb的地址放行
	Username        string   `yaml:"username"`          // 用户名
	Password        string   `yaml:"password"`          // 密码
}

/** ********************************** RPC渠道配置 ******************************** */

type WeWorkChannel struct {
	Enable     bool     `yaml:"enable"`
	Type       string   `yaml:"type"`
	MsgTypes   []string `yaml:"msg_types"`
	AgentId    string   `yaml:"agentid"`
	CorpId     string   `yaml:"corpid"`
	CorpSecret string   `yaml:"corpsecret"`
	Keywords   string   `yaml:"keywords"`
}

type TelegramChannel struct {
	Enable   bool     `yaml:"enable"`
	Type     string   `yaml:"type"`
	MsgTypes []string `yaml:"msg_types"`
	Token    string   `yaml:"token"`
	Channel  string   `yaml:"channel"`
}

/** ********************************** 标的筛选器 ******************************** */

type PairMgrConfig struct {
	Cron string `yaml:"cron"`
}

// 通用的过滤器
type CommonPairFilter struct {
	Name  string                 `yaml:"name"`
	Items map[string]interface{} `yaml:",inline"`
}

/** ********************************** 交易所部分配置 ******************************** */

// ExchangeConfig 表示交易所的配置信息
type ExchangeConfig struct {
	Name  string                    `yaml:"name"`
	Items map[string]*ExgItemConfig `yaml:",inline"`
}

// 具体交易所的配置
type ExgItemConfig struct {
	CreditProd    *CreditConfig `yaml:"credit_prod,omitempty"`
	CreditTest    *CreditConfig `yaml:"credit_test,omitempty"`
	Options       *ExgOptions   `yaml:"options,omitempty"`
	PairWhitelist []string      `yaml:"pair_whitelist,omitempty"`
	PairBlacklist []string      `yaml:"pair_blacklist,omitempty"`
	Proxies       *ProxyConfig  `yaml:"proxies,omitempty"`
}

// CreditConfig 存储 API 密钥和秘密的配置
type CreditConfig struct {
	APIKey    string `yaml:"api_key"`
	APISecret string `yaml:"api_secret"`
	BaseURL   string `yaml:"base_url,omitempty"` // omitempty 标记表示字段是可选的
	StreamURL string `yaml:"stream_url,omitempty"`
	Timeout   int    `yaml:"timeout,omitempty"`
}

// ExgOptions 交易所初始化选项
type ExgOptions struct {
}

// ProxyConfig 表示代理服务器的配置
type ProxyConfig struct {
	HTTP  string `yaml:"http"`
	HTTPS string `yaml:"https"`
}
