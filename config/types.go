package config

import (
	"github.com/banbox/banbot/core"
)

var (
	Data        Config
	Args        *CmdArgs
	Accounts    map[string]*AccountConfig // Exchange tradable account 交易所可交易账户
	BakAccounts map[string]*AccountConfig // Exchange account, not tradable 交易所账户，不可交易
	DefAcc      = "default"               // For non-real trading, the default key of the account (backtesting, simulated trading) 非实盘交易时，账户默认的key（回测、模拟交易）

	Name             string
	Loaded           bool
	Leverage         float64
	LimitVolSecs     int // How long the limit order is expected to wait for execution, in seconds 限价单预期等待多长时间成交，单位秒
	PutLimitSecs     int // Only limit orders executed within this expected time will be submitted to the exchange. 在此预期时间内成交的限价单，才提交到交易所
	OdBookTtl        int64
	StopEnterBars    int // The entry limit order will be canceled if it is not filled after the number of candles. 入场限价单超过多少个蜡烛未成交则取消
	OrderType        string
	PreFire          float64
	MarginAddRate    float64 // When trading contracts, if a loss occurs and the loss reaches this value of the initial margin ratio, additional margin will be required to avoid forced liquidation. 交易合约时，如出现亏损，亏损达到初始保证金比率的此值时，进行追加保证金，避免强平
	ChargeOnBomb     bool
	TakeOverStgy     string
	StakeAmount      float64 // The amount of a single order, the priority is lower than StakePct 单笔开单金额，优先级低于StakePct
	StakePct         float64 // Percentage of single bill amount 单笔开单金额百分比
	MaxStakeAmt      float64 // Maximum bill amount for a single transaction 单笔最大开单金额
	OpenVolRate      float64 // When opening an order without specifying a quantity, the multiple of the maximum allowed order quantity/average candle trading volume, defaults to 1 未指定数量开单时，最大允许开单数量/平均蜡烛成交量的倍数，默认1
	MinOpenRate      float64 // When the wallet balance is less than the single amount, orders are allowed to be issued when it reaches this ratio of the single amount. 钱包余额不足单笔金额时，达到单笔金额的此比例则允许开单
	BTNetCost        float64 // Order placement delay during backtesting, simulated slippage, unit seconds 回测时下单延迟，模拟滑点，单位秒
	RelaySimUnFinish bool    // 交易新品种时(回测/实盘)，是否从开始时间未平仓订单接力开始交易
	OrderBarMax      int     // 查找开始时间未平仓订单向前模拟最大bar数量
	MaxOpenOrders    int
	MaxSimulOpen     int
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
	RunPolicy        []*RunPolicyConfig
	StratPerf        *StratPerfConfig
	Pairs            []string
	PairMgr          *PairMgrConfig
	PairFilters      []*CommonPairFilter
	Exchange         *ExchangeConfig
	DataDir          string
	stratDir         string
	Database         *DatabaseConfig
	SpiderAddr       string
	APIServer        *APIServerConfig
	RPCChannels      map[string]map[string]interface{}
	Webhook          map[string]map[string]string
)

const (
	MinPairCronGapMS = 1800000 // 交易对刷新最小间隔半小时
)

var (
	noExtends = map[string]bool{
		"run_policy":     true,
		"wallet_amounts": true,
		"fatal_stop":     true,
		"watch_jobs":     true,
	}
)

// Config Is the root configuration structure 是根配置结构体
type Config struct {
	Name             string                            `yaml:"name" mapstructure:"name"`
	Env              string                            `yaml:"env" mapstructure:"env"`
	Leverage         float64                           `yaml:"leverage" mapstructure:"leverage"`
	LimitVolSecs     int                               `yaml:"limit_vol_secs" mapstructure:"limit_vol_secs"`
	PutLimitSecs     int                               `yaml:"put_limit_secs" mapstructure:"put_limit_secs"`
	MarketType       string                            `yaml:"market_type" mapstructure:"market_type"`
	ContractType     string                            `yaml:"contract_type" mapstructure:"contract_type"`
	OdBookTtl        int64                             `yaml:"odbook_ttl" mapstructure:"odbook_ttl"`
	StopEnterBars    int                               `json:"stop_enter_bars" mapstructure:"stop_enter_bars"`
	ConcurNum        int                               `json:"concur_num" mapstructure:"concur_num"`
	OrderType        string                            `yaml:"order_type" mapstructure:"order_type"`
	PreFire          float64                           `yaml:"prefire" mapstructure:"prefire"`
	MarginAddRate    float64                           `yaml:"margin_add_rate" mapstructure:"margin_add_rate"`
	ChargeOnBomb     bool                              `yaml:"charge_on_bomb" mapstructure:"charge_on_bomb"`
	TakeOverStgy     string                            `yaml:"take_over_stgy" mapstructure:"take_over_stgy"`
	StakeAmount      float64                           `yaml:"stake_amount" mapstructure:"stake_amount"`
	StakePct         float64                           `yaml:"stake_pct" mapstructure:"stake_pct"`
	MaxStakeAmt      float64                           `yaml:"max_stake_amt" mapstructure:"max_stake_amt"`
	OpenVolRate      float64                           `yaml:"open_vol_rate" mapstructure:"open_vol_rate"`
	MinOpenRate      float64                           `yaml:"min_open_rate" mapstructure:"min_open_rate"`
	BTNetCost        float64                           `yaml:"bt_net_cost" mapstructure:"bt_net_cost"`
	RelaySimUnFinish bool                              `yaml:"relay_sim_unfinish" mapstructure:"relay_sim_unfinish"`
	OrderBarMax      int                               `yaml:"order_bar_max" mapstructure:"order_bar_max"`
	MaxOpenOrders    int                               `yaml:"max_open_orders" mapstructure:"max_open_orders"`
	MaxSimulOpen     int                               `yaml:"max_simul_open" mapstructure:"max_simul_open"`
	WalletAmounts    map[string]float64                `yaml:"wallet_amounts" mapstructure:"wallet_amounts"`
	DrawBalanceOver  float64                           `yaml:"draw_balance_over" mapstructure:"draw_balance_over"`
	StakeCurrency    []string                          `yaml:"stake_currency" mapstructure:"stake_currency"`
	FatalStop        map[string]float64                `yaml:"fatal_stop" mapstructure:"fatal_stop"`
	FatalStopHours   int                               `yaml:"fatal_stop_hours" mapstructure:"fatal_stop_hours"`
	TimeRangeRaw     string                            `yaml:"timerange" mapstructure:"timerange"`
	TimeRange        *TimeTuple                        `json:"-" mapstructure:"-"`
	RunTimeframes    []string                          `yaml:"run_timeframes" mapstructure:"run_timeframes"`
	KlineSource      string                            `yaml:"kline_source" mapstructure:"kline_source"`
	WatchJobs        map[string][]string               `yaml:"watch_jobs" mapstructure:"watch_jobs"`
	RunPolicy        []*RunPolicyConfig                `yaml:"run_policy" mapstructure:"run_policy"`
	StratPerf        *StratPerfConfig                  `yaml:"strat_perf" mapstructure:"strat_perf"`
	Pairs            []string                          `yaml:"pairs" mapstructure:"pairs"`
	PairMgr          *PairMgrConfig                    `yaml:"pairmgr" mapstructure:"pairmgr"`
	PairFilters      []*CommonPairFilter               `yaml:"pairlists" mapstructure:"pairlists"`
	Exchange         *ExchangeConfig                   `yaml:"exchange" mapstructure:"exchange"`
	Database         *DatabaseConfig                   `yaml:"database" mapstructure:"database"`
	SpiderAddr       string                            `yaml:"spider_addr" mapstructure:"spider_addr"`
	APIServer        *APIServerConfig                  `yaml:"api_server" mapstructure:"api_server"`
	RPCChannels      map[string]map[string]interface{} `yaml:"rpc_channels" mapstructure:"rpc_channels"`
	Webhook          map[string]map[string]string      `yaml:"webhook" mapstructure:"webhook"`
}

// The strategy to run, multiple strategies can be run at the same time 运行的策略，可以多个策略同时运行
type RunPolicyConfig struct {
	Name          string                        `yaml:"name" mapstructure:"name"`
	Filters       []*CommonPairFilter           `yaml:"filters" mapstructure:"filters"`
	RunTimeframes []string                      `yaml:"run_timeframes" mapstructure:"run_timeframes"`
	MaxPair       int                           `yaml:"max_pair" mapstructure:"max_pair"`
	MaxOpen       int                           `yaml:"max_open" mapstructure:"max_open"`
	MaxSimulOpen  int                           `yaml:"max_simul_open" mapstructure:"max_simul_open"`
	OrderBarMax   int                           `yaml:"order_bar_max" mapstructure:"order_bar_max"`
	StakeRate     float64                       `yaml:"stake_rate" mapstructure:"stake_rate"`
	Dirt          string                        `yaml:"dirt" mapstructure:"dirt"`
	StratPerf     *StratPerfConfig              `yaml:"strat_perf" mapstructure:"strat_perf"`
	Pairs         []string                      `yaml:"pairs" mapstructure:"pairs"`
	Params        map[string]float64            `yaml:"params" mapstructure:"params"`
	PairParams    map[string]map[string]float64 `yaml:"pair_params" mapstructure:"pair_params"`
	defs          map[string]*core.Param
	Score         float64
}

type StratPerfConfig struct {
	Enable    bool    `yaml:"enable" mapstructure:"enable"`
	MinOdNum  int     `yaml:"min_od_num" mapstructure:"min_od_num"`
	MaxOdNum  int     `yaml:"max_od_num" mapstructure:"max_od_num"`
	MinJobNum int     `yaml:"min_job_num" mapstructure:"min_job_num"`
	MidWeight float64 `yaml:"mid_weight" mapstructure:"mid_weight"`
	BadWeight float64 `yaml:"bad_weight" mapstructure:"bad_weight"`
}

type DatabaseConfig struct {
	Url         string `yaml:"url" mapstructure:"url"`
	Retention   string `yaml:"retention" mapstructure:"retention"`
	MaxPoolSize int    `yaml:"max_pool_size" mapstructure:"max_pool_size"`
	AutoCreate  bool   `yaml:"auto_create" mapstructure:"auto_create"`
}

type APIServerConfig struct {
	Enable       bool          `yaml:"enable" mapstructure:"enable"`                 // Whether to enable 是否启用
	BindIPAddr   string        `yaml:"bind_ip" mapstructure:"bind_ip"`               // Binding address, 0.0.0.0 means exposed to the public network 绑定地址，0.0.0.0表示暴露到公网
	Port         int           `yaml:"port" mapstructure:"port"`                     // LOCAL LISTENING PORT 本地监听端口
	Verbosity    string        `yaml:"verbosity" mapstructure:"verbosity"`           // Detail level 详细程度
	JWTSecretKey string        `yaml:"jwt_secret_key" mapstructure:"jwt_secret_key"` // Key used for password encryption 用于密码加密的密钥
	CORSOrigins  []string      `yaml:"CORS_origins" mapstructure:"CORS_origins"`     // When accessing banweb, you need to add the address of banweb here to allow access. banweb访问时，要这里添加banweb的地址放行
	Users        []*UserConfig `yaml:"users" mapstructure:"users"`                   // Login user 登录用户
}

type UserConfig struct {
	Username    string            `yaml:"user" mapstructure:"user"`           // 用户名
	Password    string            `yaml:"pwd" mapstructure:"pwd"`             // 密码
	AccRoles    map[string]string `yaml:"acc_roles" mapstructure:"acc_roles"` // Role permissions for different accounts 对不同账户的角色权限
	ExpireHours float64           `yaml:"exp_hours" mapstructure:"exp_hours"` // Token expiration time, default 168 hours token过期时间，默认168小时
}

/** ********************************** RPC Channel Configuration 渠道配置 ******************************** */

type WeWorkChannel struct {
	Enable     bool     `yaml:"enable" mapstructure:"enable"`
	Type       string   `yaml:"type" mapstructure:"type"`
	MsgTypes   []string `yaml:"msg_types" mapstructure:"msg_types"`
	AgentId    string   `yaml:"agentid" mapstructure:"agentid"`
	CorpId     string   `yaml:"corpid" mapstructure:"corpid"`
	CorpSecret string   `yaml:"corpsecret" mapstructure:"corpsecret"`
	Keywords   string   `yaml:"keywords" mapstructure:"keywords"`
}

type TelegramChannel struct {
	Enable   bool     `yaml:"enable" mapstructure:"enable"`
	Type     string   `yaml:"type" mapstructure:"type"`
	MsgTypes []string `yaml:"msg_types" mapstructure:"msg_types"`
	Token    string   `yaml:"token" mapstructure:"token"`
	Channel  string   `yaml:"channel" mapstructure:"channel"`
}

/** ********************************** Symbol FILTER标的筛选器  ******************************** */

type PairMgrConfig struct {
	Cron string `yaml:"cron" mapstructure:"cron"`
	// Offset limited quantity selection. 偏移限定数量选择。
	Offset int `yaml:"offset" mapstructure:"offset,omitempty"`
	// Limit the number of currencies 限制币种数量
	Limit int `yaml:"limit" mapstructure:"limit,omitempty"`
	// apply filters to static pairs force
	ForceFilters bool `yaml:"force_filters" mapstructure:"force_filters,omitempty"`
	// hole/close 品种切换时保留还是退出仓位
	PosOnRotation string `yaml:"pos_on_rotation" mapstructure:"pos_on_rotation"`
}

// UNIVERSAL FILTER 通用的过滤器
type CommonPairFilter struct {
	Name  string                 `yaml:"name" mapstructure:"name"`
	Items map[string]interface{} `mapstructure:",remain"`
}

/** ********************************** Exchange part configuration 交易所部分配置 ******************************** */

// ExchangeConfig Represents the configuration information of the exchange 表示交易所的配置信息
type ExchangeConfig struct {
	Name  string                    `yaml:"name" mapstructure:"name"`
	Items map[string]*ExgItemConfig `yaml:",inline" mapstructure:",remain"`
}

// Configuration of specific exchanges 具体交易所的配置
type ExgItemConfig struct {
	AccountProds map[string]*AccountConfig `yaml:"account_prods,omitempty" mapstructure:"account_prods,omitempty"`
	AccountTests map[string]*AccountConfig `yaml:"account_tests,omitempty" mapstructure:"account_tests,omitempty"`
	Options      map[string]interface{}    `yaml:"options,omitempty" mapstructure:"options,omitempty"`
}

// AccountConfig Configuration to store API keys and secrets 存储 API 密钥和秘密的配置
type AccountConfig struct {
	APIKey      string  `yaml:"api_key" mapstructure:"api_key"`
	APISecret   string  `yaml:"api_secret" mapstructure:"api_secret"`
	NoTrade     bool    `yaml:"no_trade" mapstructure:"no_trade"`
	MaxStakeAmt float64 `yaml:"max_stake_amt" mapstructure:"max_stake_amt"` // Maximum amount allowed for a single transaction 允许的单笔最大金额
	StakeRate   float64 `yaml:"stake_rate" mapstructure:"stake_rate"`       // Multiple of billing amount relative to benchmark  相对基准的开单金额倍数
	StakePctAmt float64 // The amount currently allowed when billing by percentage按百分比开单时，当前允许的金额
	Leverage    float64 `yaml:"leverage" mapstructure:"leverage"`
}

type TimeTuple struct {
	StartMS int64
	EndMS   int64
}

type MarketRange struct {
	Exchange  string `yaml:"exchange"`
	ExgReal   string `yaml:"exg_real"`
	Market    string `yaml:"market"`
	TimeRange string `yaml:"time_range"`
}

type MarketSymbolsRange struct {
	*MarketRange `yaml:",inline"`
	Symbols      []string `yaml:"symbols"`
}

type MarketTFSymbolsRange struct {
	*MarketSymbolsRange `yaml:",inline"`
	TimeFrames          []string `yaml:"timeframes"`
}

// ExportConfig represents the export configuration
type ExportConfig struct {
	Klines     []*MarketTFSymbolsRange `yaml:"klines"`
	AdjFactors []*MarketSymbolsRange   `yaml:"adj_factors"`
	Calendars  []*MarketRange          `yaml:"calendars"`
}
