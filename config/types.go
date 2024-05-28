package config

import "regexp"

var (
	Data        Config
	Args        *CmdArgs
	Accounts    map[string]*AccountConfig // 交易所可交易账户
	BakAccounts map[string]*AccountConfig // 交易所账户，不可交易
	DefAcc      = "default"               // 非实盘交易时，账户默认的key（回测、模拟交易）

	Name             string
	Loaded           bool
	Debug            bool
	NoDB             bool
	Leverage         float64
	LimitVolSecs     int // 限价单预期等待多长时间成交，单位秒
	PutLimitSecs     int // 在此预期时间内成交的限价单，才提交到交易所
	OdBookTtl        int64
	StopEnterBars    int // 入场限价单超过多少个蜡烛未成交则取消
	OrderType        string
	PreFire          float64
	MarginAddRate    float64 // 交易合约时，如出现亏损，亏损达到初始保证金比率的此值时，进行追加保证金，避免强平
	ChargeOnBomb     bool
	TakeOverStgy     string
	StakeAmount      float64 // 单笔开单金额，优先级低于StakePct
	StakePct         float64 // 单笔开单金额百分比
	MaxStakeAmt      float64 // 单笔最大开单金额
	OpenVolRate      float64 // 未指定数量开单时，最大允许开单数量/平均蜡烛成交量的倍数，默认1
	MinOpenRate      float64 // 钱包余额不足单笔金额时，达到单笔金额的此比例则允许开单
	BTNetCost        float64 // 回测时下单延迟，模拟滑点，单位秒
	MaxOpenOrders    int
	WalletAmounts    map[string]float64
	DrawBalanceOver  float64
	StakeCurrency    []string
	StakeCurrencyMap map[string]bool
	FatalStop        map[int]float64
	FatalStopHours   int
	TimeRange        *TimeTuple
	RunTimeframes    []string
	KlineSource      string
	WatchJobs        map[string][]string
	RunPolicy        map[string]*RunPolicyConfig
	Pairs            []string
	PairMgr          *PairMgrConfig
	PairFilters      []*CommonPairFilter
	Exchange         *ExchangeConfig
	DataDir          string
	Database         *DatabaseConfig
	SpiderAddr       string
	APIServer        *APIServerConfig
	RPCChannels      map[string]map[string]interface{}
	Webhook          map[string]map[string]string

	ReClientID *regexp.Regexp // 正则匹配ClientID，检查是否是机器人下单
)

var (
	noExtends = map[string]bool{
		"run_policy":     true,
		"wallet_amounts": true,
		"fatal_stop":     true,
		"watch_jobs":     true,
	}
)

// Config 是根配置结构体
type Config struct {
	Name            string                            `yaml:"name" mapstructure:"name"`
	Env             string                            `yaml:"env" mapstructure:"env"`
	Leverage        float64                           `yaml:"leverage" mapstructure:"leverage"`
	LimitVolSecs    int                               `yaml:"limit_vol_secs" mapstructure:"limit_vol_secs"`
	PutLimitSecs    int                               `yaml:"put_limit_secs" mapstructure:"put_limit_secs"`
	MarketType      string                            `yaml:"market_type" mapstructure:"market_type"`
	ContractType    string                            `yaml:"contract_type" mapstructure:"contract_type"`
	OdBookTtl       int64                             `yaml:"odbook_ttl" mapstructure:"odbook_ttl"`
	StopEnterBars   int                               `json:"stop_enter_bars" mapstructure:"stop_enter_bars"`
	ConcurNum       int                               `json:"concur_num" mapstructure:"concur_num"`
	OrderType       string                            `yaml:"order_type" mapstructure:"order_type"`
	PreFire         float64                           `yaml:"prefire" mapstructure:"prefire"`
	MarginAddRate   float64                           `yaml:"margin_add_rate" mapstructure:"margin_add_rate"`
	ChargeOnBomb    bool                              `yaml:"charge_on_bomb" mapstructure:"charge_on_bomb"`
	TakeOverStgy    string                            `yaml:"take_over_stgy" mapstructure:"take_over_stgy"`
	StakeAmount     float64                           `yaml:"stake_amount" mapstructure:"stake_amount"`
	StakePct        float64                           `yaml:"stake_pct" mapstructure:"stake_pct"`
	MaxStakeAmt     float64                           `yaml:"max_stake_amt" mapstructure:"max_stake_amt"`
	OpenVolRate     float64                           `yaml:"open_vol_rate" mapstructure:"open_vol_rate"`
	MinOpenRate     float64                           `yaml:"min_open_rate" mapstructure:"min_open_rate"`
	BTNetCost       float64                           `yaml:"bt_net_cost" mapstructure:"bt_net_cost"`
	MaxOpenOrders   int                               `yaml:"max_open_orders" mapstructure:"max_open_orders"`
	WalletAmounts   map[string]float64                `yaml:"wallet_amounts" mapstructure:"wallet_amounts"`
	DrawBalanceOver float64                           `yaml:"draw_balance_over" mapstructure:"draw_balance_over"`
	StakeCurrency   []string                          `yaml:"stake_currency" mapstructure:"stake_currency"`
	FatalStop       map[string]float64                `yaml:"fatal_stop" mapstructure:"fatal_stop"`
	FatalStopHours  int                               `yaml:"fatal_stop_hours" mapstructure:"fatal_stop_hours"`
	TimeRangeRaw    string                            `yaml:"timerange" mapstructure:"timerange"`
	TimeRange       *TimeTuple                        `json:"-" mapstructure:"-"`
	RunTimeframes   []string                          `yaml:"run_timeframes" mapstructure:"run_timeframes"`
	KlineSource     string                            `yaml:"kline_source" mapstructure:"kline_source"`
	WatchJobs       map[string][]string               `yaml:"watch_jobs" mapstructure:"watch_jobs"`
	RunPolicy       map[string]*RunPolicyConfig       `yaml:"run_policy" mapstructure:"run_policy"`
	StrtgPerf       *StrtgPerfConfig                  `yaml:"strtg_perf" mapstructure:"strtg_perf"`
	Pairs           []string                          `yaml:"pairs" mapstructure:"pairs"`
	PairMgr         *PairMgrConfig                    `yaml:"pairmgr" mapstructure:"pairmgr"`
	PairFilters     []*CommonPairFilter               `yaml:"pairlists" mapstructure:"pairlists"`
	Exchange        *ExchangeConfig                   `yaml:"exchange" mapstructure:"exchange"`
	Database        *DatabaseConfig                   `yaml:"database" mapstructure:"database"`
	SpiderAddr      string                            `yaml:"spider_addr" mapstructure:"spider_addr"`
	APIServer       *APIServerConfig                  `yaml:"api_server" mapstructure:"api_server"`
	RPCChannels     map[string]map[string]interface{} `yaml:"rpc_channels" mapstructure:"rpc_channels"`
	Webhook         map[string]map[string]string      `yaml:"webhook" mapstructure:"webhook"`
}

// 运行的策略，可以多个策略同时运行
type RunPolicyConfig struct {
	Name          string              `yaml:"name" mapstructure:"name"`
	Filters       []*CommonPairFilter `yaml:"filters" mapstructure:"filters"`
	RunTimeframes []string            `yaml:"run_timeframes" mapstructure:"run_timeframes"`
	MaxPair       int                 `yaml:"max_pair" mapstructure:"max_pair"`
	MaxOpen       int                 `yaml:"max_open" mapstructure:"max_open"`
	StrtgPerf     *StrtgPerfConfig    `yaml:"strtg_perf" mapstructure:"strtg_perf"`
	Pairs         []string            `yaml:"pairs" mapstructure:"pairs"`
}

type StrtgPerfConfig struct {
	Enable    bool    `yaml:"enable" mapstructure:"enable"`
	MinOdNum  int     `yaml:"min_od_num" mapstructure:"min_od_num"`
	MaxOdNum  int     `yaml:"max_od_num" mapstructure:"max_od_num"`
	MinJobNum int     `yaml:"min_job_num" mapstructure:"min_job_num"`
	MidWeight float64 `yaml:"mid_weight" mapstructure:"mid_weight"`
	BadWeight float64 `yaml:"bad_weight" mapstructure:"bad_weight"`
}

type DatabaseConfig struct {
	Url         string `yaml:"url"`
	Retention   string `yaml:"retention"`
	MaxPoolSize int    `yaml:"max_pool_size"`
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
	Cron string `yaml:"cron" mapstructure:"cron"`
	// 偏移限定数量选择。
	Offset int `yaml:"offset" mapstructure:"offset,omitempty"`
	// 限制币种数量
	Limit int `yaml:"limit" mapstructure:"limit,omitempty"`
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
	AccountProds map[string]*AccountConfig `yaml:"account_prods,omitempty" mapstructure:"account_prods,omitempty"`
	AccountTests map[string]*AccountConfig `yaml:"account_tests,omitempty" mapstructure:"account_tests,omitempty"`
	Options      map[string]interface{}    `yaml:"options,omitempty" mapstructure:"options,omitempty"`
}

// AccountConfig 存储 API 密钥和秘密的配置
type AccountConfig struct {
	APIKey      string  `yaml:"api_key" mapstructure:"api_key"`
	APISecret   string  `yaml:"api_secret" mapstructure:"api_secret"`
	NoTrade     bool    `yaml:"no_trade" mapstructure:"no_trade"`
	MaxStakeAmt float64 `yaml:"max_stake_amt" mapstructure:"max_stake_amt"` // 允许的单笔最大金额
	StakeRate   float64 `yaml:"stake_rate" mapstructure:"stake_rate"`       // 相对基准的开单金额倍数
	StakePctAmt float64 // 按百分比开单时，当前允许的金额
	Leverage    float64 `yaml:"leverage" mapstructure:"leverage"`
}

type TimeTuple struct {
	StartMS int64
	EndMS   int64
}
