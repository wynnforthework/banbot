package core

var (
	BotName      string                        // 当前机器人名称
	RunMode      string                        // prod/dry_run/backtest
	RunEnv       string                        //prod/test
	StartAt      uint64                        // 启动时间，13位时间戳
	IsWarmUp     bool                          //是否当前处于预热状态
	TFSecs       = make([]*TFSecTuple, 0)      // 所有涉及的时间周期
	ExgName      string                        // 交易所名称
	Market       string                        //当前市场
	StgPairTfs   = make([]*StgPairTf, 0, 5)    //策略、标的、周期
	Pairs        = make(map[string]bool)       //全局所有的标的
	PairTfScores = make(map[string][]*TfScore) // tf scores for pairs
	ForbidPairs  = make(map[string]bool)       // 禁止交易的币种
	BookPairs    = make(map[string]bool)       //监听交易对的币种

)

var (
	DownOHLCVParallel = 3
)

const (
	SecsMin  = 60
	SecsHour = SecsMin * 60
	SecsDay  = SecsHour * 24
	SecsWeek = SecsDay * 7
	SecsMon  = SecsDay * 30
	SecsYear = SecsDay * 365
)

const (
	MSMinStamp = 157766400000 // 1975-01-01T00:00:00.000Z
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
	barPrices = make(map[string]float64) //# 来自bar的每个币的最新价格，仅用于回测等。键可以是交易对，也可以是币的code
	prices    = make(map[string]float64) //交易对的最新订单簿价格，仅用于实时模拟或实盘。键可以是交易对，也可以是币的code

)
