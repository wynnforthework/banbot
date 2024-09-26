package core

import (
	"context"
	"github.com/banbox/banexg"
	"github.com/robfig/cron/v3"
	"io"
	"sync"
)

var (
	RunMode      string                               // live / backtest / other
	RunEnv       string                               // prod / test / dry_run
	StartAt      int64                                // 启动时间，13位时间戳
	EnvReal      bool                                 // 是否是提交到交易所真实订单模式run_env:prod/test
	LiveMode     bool                                 // 是否是实时模式：实盘+模拟运行
	TFSecs       map[string]int                       // 所有涉及的时间周期
	ExgName      string                               // 交易所名称
	Market       string                               // 当前市场
	IsContract   bool                                 // 当前市场是否是合约市场, linear/inverse/option
	CheckWallets bool                                 // 当前是否应该更新钱包
	ContractType string                               // 当前合约类型
	StgPairTfs   = make(map[string]map[string]string) // 策略: 标的: 周期
	Pairs        []string                             // 全局所有的标的，按标的刷新后的顺序
	PairsMap     = make(map[string]bool)              // 全局所有的标的
	ForbidPairs  = make(map[string]bool)              // 禁止交易的币种
	NoEnterUntil = make(map[string]int64)             // account: 禁止开单的截止13位时间戳
	BookPairs    = make(map[string]bool)              // 监听交易对的币种
	PairCopiedMs = map[string][2]int64{}              // 所有标的从爬虫收到K线的最新时间，以及等待间隔，用于判断是否有长期未收到的。
	TfPairHits   = map[string]map[string]int{}        // tf[pair[hits]]一段时间内各周期各币种的bar数量，用于定时输出
	JobPerfs     = make(map[string]*JobPerf)          // stagy_pair_tf: JobPerf 记录任务的开单金额比率，胜率低的要减少开单金额
	StagyPerfSta = make(map[string]*PerfSta)          // stagy: Job任务状态
	LastBarMs    int64                                // 上次收到bar的结束时间，13位时间戳
	OdBooks      = map[string]*banexg.OrderBook{}     // 缓存所有从爬虫收到的订单簿
	NumTaCache   = 1500                               // 指标计算时缓存的历史值数量，默认1500
	Cron         = cron.New(cron.WithSeconds())       // 使用cron定时运行任务

	ExitCalls []func()  // 停止执行的回调
	MemOut    io.Writer // 进行内存profile的输出

	ConcurNum = 2 // 最大同时下载K线任务数，过大会出现429限流
	Version   = "0.1.1"
)

const (
	SecsMin  = 60
	SecsHour = SecsMin * 60
	SecsDay  = SecsHour * 24
	SecsWeek = SecsDay * 7
	SecsMon  = SecsDay * 30
	SecsQtr  = SecsMon * 3
	SecsYear = SecsDay * 365
)

const (
	MSMinStamp = 157766400000 // 1975-01-01T00:00:00.000Z
)

const (
	MinStakeAmount = 10 // 最小开单金额
	StepTotal      = 1000
	KBatchSize     = 900 // 单次请求交易所最大返回K线数量, 1000时api权重过大
	DefaultDateFmt = "2006-01-02 15:04:05"
	DelayBatchMS   = 3000  // 批量逻辑推迟的毫秒数
	PrefMinRate    = 0.001 // job最低开仓比率，直接使用MinStakeAmount开仓
	AmtDust        = 1e-8
)

const (
	RunModeLive     = "live"
	RunModeBackTest = "backtest"
	RunModeOther    = "other"
)

const (
	RunEnvProd   = "prod"
	RunEnvTest   = "test"
	RunEnvDryRun = "dry_run"
)

const (
	OdDirtShort = iota - 1
	OdDirtBoth
	OdDirtLong
)

const (
	EnterTagUnknown  = "unknown"
	EnterTagUserOpen = "user_open"
	EnterTagThird    = "third"
)

const (
	ExitTagUnknown     = "unknown"
	ExitTagCancel      = "cancel"
	ExitTagBotStop     = "bot_stop"
	ExitTagForceExit   = "force_exit"
	ExitTagUserExit    = "user_exit"
	ExitTagThird       = "third"
	ExitTagFatalErr    = "fatal_err"
	ExitTagPairDel     = "pair_del"
	ExitTagStopLoss    = "stop_loss"
	ExitTagSLTake      = "sl_take"
	ExitTagTakeProfit  = "take_profit"
	ExitTagDrawDown    = "draw_down"
	ExitTagDataStuck   = "data_stuck"
	ExitTagLiquidation = "liquidation"
	ExitTagEnvEnd      = "env_end"
	ExitTagEntExp      = "ent_exp" // enter limit expired
)

var (
	barPrices     = make(map[string]float64) //# 来自bar的每个币的最新价格，仅用于回测等。键可以是交易对，也可以是币的code
	prices        = make(map[string]float64) //交易对的最新订单簿价格，仅用于实时模拟或实盘。键可以是交易对，也可以是币的code
	lockPrices    sync.RWMutex
	lockBarPrices sync.RWMutex
	Ctx           context.Context // 用于全部goroutine同时停止
	StopAll       func()          // 停止全部机器人线程
	BotRunning    bool            // 机器人是否正在运行
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
	OrderTypeLimitMaker
)

const (
	AdjNone   = 1
	AdjFront  = 2
	AdjBehind = 3
)

const (
	VTypeUniform = iota // 均匀线性分布
	VTypeNorm           // 正态分布，指定均值和标准差
)
