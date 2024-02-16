package core

import (
	"context"
	"github.com/banbox/banexg"
)

var (
	BotName      string                           // 当前机器人名称
	RunMode      string                           // prod/dry_run/backtest
	RunEnv       string                           //prod/test
	StartAt      uint64                           // 启动时间，13位时间戳
	IsWarmUp     bool                             //是否当前处于预热状态
	TFSecs       []*TFSecTuple                    // 所有涉及的时间周期
	ExgName      string                           // 交易所名称
	Market       string                           //当前市场
	ContractType string                           // 当前合约类型
	StgPairTfs   []*StgPairTf                     //策略、标的、周期
	Pairs        []string                         //全局所有的标的
	PairsMap     = make(map[string]bool)          //全局所有的标的
	PairTfScores = make(map[string][]*TfScore)    // tf scores for pairs
	ForbidPairs  = make(map[string]bool)          // 禁止交易的币种
	NoEnterUntil int64                            // 禁止开单的截止13位时间戳
	BookPairs    = make(map[string]bool)          //监听交易对的币种
	PairCopiedMs = map[string][2]int64{}          // 所有标的从爬虫收到K线的最新时间，以及等待间隔，用于判断是否有长期未收到的。
	TfPairHits   = map[string]map[string]int{}    // tf[pair[hits]]一段时间内各周期各币种的bar数量，用于定时输出
	LastBarMs    int64                            // 上次收到bar的结束时间，13位时间戳
	OdBooks      = map[string]*banexg.OrderBook{} //缓存所有从爬虫收到的订单簿
	NumTaCache   = 1500                           // 指标计算时缓存的历史值数量，默认1500
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
	OdDirtShort = iota - 1
	OdDirtBoth
	OdDirtLong
)

const (
	BotStateRunning = 1
	BotStateStopped = 2
)

const (
	ExitTagUnknown     = "unknown"
	ExitTagBotStop     = "bot_stop"
	ExitTagForceExit   = "force_exit"
	ExitTagUserExit    = "user_exit"
	ExitTagThird       = "third"
	ExitTagFatalErr    = "fatal_err"
	ExitTagPairDel     = "pair_del"
	ExitTagStopLoss    = "stop_loss"
	ExitTagTakeProfit  = "take_profit"
	ExitTagDataStuck   = "data_stuck"
	ExitTagLiquidation = "liquidation"
)

var (
	barPrices = make(map[string]float64) //# 来自bar的每个币的最新价格，仅用于回测等。键可以是交易对，也可以是币的code
	prices    = make(map[string]float64) //交易对的最新订单簿价格，仅用于实时模拟或实盘。键可以是交易对，也可以是币的code
	Ctx       context.Context            // 用于全部goroutine同时停止
	StopAll   func()                     // 停止全部机器人线程
)

var (
	OrderTypeEnums = []string{"", banexg.OdTypeMarket, banexg.OdTypeLimit, banexg.OdTypeStopLoss,
		banexg.OdTypeStopLossLimit, banexg.OdTypeTakeProfit, banexg.OdTypeTakeProfitLimit,
		banexg.OdTypeStop, banexg.OdTypeLimitMaker}
)

const (
	OrderTypeEmpty = iota
	OrderTypeMarket
	OrderTypeLimit
	OrderTypeStopLoss
	OrderTypeStopLossLimit
	OrderTypeTakeProfit
	OrderTypeTakeProfitLimit
	OrderTypeStop
	OrderTypeLimitMaker
)
