package config

var (
	data Config
	Args *CmdArgs

	Name            string
	Loaded          bool
	Debug           bool
	NoDB            bool
	Leverage        int
	LimitVolSecs    int
	MaxMarketRate   float64
	OdBookTtl       uint16
	OrderType       string
	PreFire         float64
	MarginAddRate   float64
	ChargeOnBomb    bool
	AutoEditLimit   bool
	TakeOverStgy    string
	StakeAmount     float64
	MinOpenRate     float64
	MaxOpenOrders   int
	WalletAmounts   map[string]float64
	DrawBalanceOver float64
	StakeCurrency   []string
	FatalStop       map[string]float64
	FatalStopHours  int
	TimeRange       *TimeTuple
	WsStamp         *string
	RunTimeframes   []string
	KlineSource     string
	WatchJobs       map[string][]string
	RunPolicy       []*RunPolicyConfig
	Pairs           []string
	PairMgr         *PairMgrConfig
	PairFilters     []*CommonPairFilter
	Exchange        *ExchangeConfig
	DataDir         string
	ExgDataMap      map[string]string
	Database        *DatabaseConfig
	SpiderAddr      string
	APIServer       *APIServerConfig
	RPCChannels     map[string]map[string]interface{}
	Webhook         map[string]map[string]string
)

// Config 是根配置结构体
type Config struct {
	Name            string                            `yaml:"name" mapstructure:"name"`
	Env             string                            `yaml:"env" mapstructure:"env"`
	RunMode         string                            `yaml:"run_mode" mapstructure:"run_mode"`
	Leverage        int                               `yaml:"leverage" mapstructure:"leverage"`
	LimitVolSecs    int                               `yaml:"limit_vol_secs" mapstructure:"limit_vol_secs"`
	MarketType      string                            `yaml:"market_type" mapstructure:"market_type"`
	ContractType    string                            `yaml:"contract_type" mapstructure:"contract_type"`
	MaxMarketRate   float64                           `yaml:"max_market_rate" mapstructure:"max_market_rate"`
	OdBookTtl       uint16                            `yaml:"odbook_ttl" mapstructure:"odbook_ttl"`
	OrderType       string                            `yaml:"order_type" mapstructure:"order_type"`
	PreFire         float64                           `yaml:"prefire" mapstructure:"prefire"`
	MarginAddRate   float64                           `yaml:"margin_add_rate" mapstructure:"margin_add_rate"`
	ChargeOnBomb    bool                              `yaml:"charge_on_bomb" mapstructure:"charge_on_bomb"`
	AutoEditLimit   bool                              `yaml:"auto_edit_limit" mapstructure:"auto_edit_limit"`
	TakeOverStgy    string                            `yaml:"take_over_stgy" mapstructure:"take_over_stgy"`
	StakeAmount     float64                           `yaml:"stake_amount" mapstructure:"stake_amount"`
	MinOpenRate     float64                           `yaml:"min_open_rate" mapstructure:"min_open_rate"`
	MaxOpenOrders   int                               `yaml:"max_open_orders" mapstructure:"max_open_orders"`
	WalletAmounts   map[string]float64                `yaml:"wallet_amounts" mapstructure:"wallet_amounts"`
	DrawBalanceOver float64                           `yaml:"draw_balance_over" mapstructure:"draw_balance_over"`
	StakeCurrency   []string                          `yaml:"stake_currency" mapstructure:"stake_currency"`
	FatalStop       map[string]float64                `yaml:"fatal_stop" mapstructure:"fatal_stop"`
	FatalStopHours  int                               `yaml:"fatal_stop_hours" mapstructure:"fatal_stop_hours"`
	TimeRangeRaw    string                            `yaml:"timerange" mapstructure:"timerange"`
	TimeRange       *TimeTuple                        `json:"-" mapstructure:"-"`
	WsStamp         *string                           `yaml:"ws_stamp" mapstructure:"ws_stamp"`
	RunTimeframes   []string                          `yaml:"run_timeframes" mapstructure:"run_timeframes"`
	KlineSource     string                            `yaml:"kline_source" mapstructure:"kline_source"`
	WatchJobs       map[string][]string               `yaml:"watch_jobs" mapstructure:"watch_jobs"`
	RunPolicy       []*RunPolicyConfig                `yaml:"run_policy" mapstructure:"run_policy"`
	Pairs           []string                          `yaml:"pairs" mapstructure:"pairs"`
	PairMgr         *PairMgrConfig                    `yaml:"pairmgr" mapstructure:"pairmgr"`
	PairFilters     []*CommonPairFilter               `yaml:"pairlists" mapstructure:"pairlists"`
	Exchange        *ExchangeConfig                   `yaml:"exchange" mapstructure:"exchange"`
	ExgDataMap      map[string]string                 `yaml:"exg_data_map" mapstructure:"exg_data_map"`
	Database        *DatabaseConfig                   `yaml:"database" mapstructure:"database"`
	SpiderAddr      string                            `yaml:"spider_addr" mapstructure:"spider_addr"`
	APIServer       *APIServerConfig                  `yaml:"api_server" mapstructure:"api_server"`
	RPCChannels     map[string]map[string]interface{} `yaml:"rpc_channels" mapstructure:"rpc_channels"`
	Webhook         map[string]map[string]string      `yaml:"webhook" mapstructure:"webhook"`
}

// 运行的策略，可以多个策略同时运行
type RunPolicyConfig struct {
	Name          string   `yaml:"name" mapstructure:"name"`
	RunTimeframes []string `yaml:"run_timeframes" mapstructure:"run_timeframes"`
	MaxPair       int      `yaml:"max_pair" mapstructure:"max_pair"`
}

type DatabaseConfig struct {
	Url       string `yaml:"url"`
	Retention string `yaml:"retention"`
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
	Items map[string]interface{} `mapstructure:",remain"`
}

/** ********************************** 交易所部分配置 ******************************** */

// ExchangeConfig 表示交易所的配置信息
type ExchangeConfig struct {
	Name  string                    `yaml:"name"`
	Items map[string]*ExgItemConfig `mapstructure:",remain"`
}

// 具体交易所的配置
type ExgItemConfig struct {
	CreditProds map[string]*CreditConfig `yaml:"credit_prods,omitempty" mapstructure:"credit_prods,omitempty"`
	CreditTests map[string]*CreditConfig `yaml:"credit_tests,omitempty" mapstructure:"credit_tests,omitempty"`
	Options     map[string]interface{}   `yaml:"options,omitempty" mapstructure:"options,omitempty"`
	WhitePairs  []string                 `yaml:"white_pairs,omitempty" mapstructure:"white_pairs,omitempty"`
	BlackPairs  []string                 `yaml:"black_pairs,omitempty" mapstructure:"black_pairs,omitempty"`
}

// CreditConfig 存储 API 密钥和秘密的配置
type CreditConfig struct {
	APIKey      string  `yaml:"api_key" mapstructure:"api_key"`
	APISecret   string  `yaml:"api_secret" mapstructure:"api_secret"`
	StakeAmount float64 `yaml:"stake_amount" mapstructure:"stake_amount"`
}

type TimeTuple struct {
	StartMS int64
	EndMS   int64
}
