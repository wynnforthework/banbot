package strat

import (
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg"
	ta "github.com/banbox/banta"
)

type CalcDDExitRate func(s *StagyJob, od *orm.InOutOrder, maxChg float64) float64
type PickTimeFrameFunc func(symbol string, tfScores []*core.TfScore) string
type FnOdChange func(acc string, od *orm.InOutOrder, evt int)

type TradeStagy struct {
	Name          string
	Version       int
	WarmupNum     int
	MinTfScore    float64 // 最小时间周期质量，默认0.8
	WatchBook     bool
	DrawDownExit  bool
	BatchInOut    bool    // 是否批量执行入场/出场
	BatchInfo     bool    // 是否对OnInfoBar后执行批量处理
	StakeRate     float64 // 相对基础金额开单倍率
	StopEnterBars int
	EachMaxLong   int      // max number of long open orders for one pair
	EachMaxShort  int      // max number of short open orders for one pair
	AllowTFs      []string // 允许运行的时间周期，不提供时使用全局配置
	Outputs       []string // 策略输出的文本文件内容，每个字符串是一行
	Policy        *config.RunPolicyConfig

	OnPairInfos         func(s *StagyJob) []*PairSub
	OnStartUp           func(s *StagyJob)
	OnBar               func(s *StagyJob)
	OnInfoBar           func(s *StagyJob, e *ta.BarEnv, pair, tf string)   // 其他依赖的bar数据
	OnTrades            func(s *StagyJob, trades []*banexg.Trade)          // 逐笔交易数据
	OnBatchJobs         func(jobs []*StagyJob)                             // 当前时间所有标的job，用于批量开单/平仓
	OnBatchInfos        func(jobs map[string]*StagyJob)                    // 当前时间所有info标的job，用于批量处理
	OnCheckExit         func(s *StagyJob, od *orm.InOutOrder) *ExitReq     // 自定义订单退出逻辑
	OnOrderChange       func(s *StagyJob, od *orm.InOutOrder, chgType int) // 订单更新回调
	GetDrawDownExitRate CalcDDExitRate                                     // 计算跟踪止盈回撤退出的比率
	PickTimeFrame       PickTimeFrameFunc                                  // 为指定币选择适合的交易周期
	OnShutDown          func(s *StagyJob)                                  // 机器人停止时回调
}

const (
	OdChgNew       = iota // 新订单
	OdChgEnter            // 创建入场订单
	OdChgEnterFill        // 订单入场完成
	OdChgExit             // 订单请求退出
	OdChgExitFill         // 订单退出完成
)

const (
	BatchTypeEnter = iota
	BatchTypeInfo
)

type BatchTask struct {
	Job  *StagyJob
	Type int
}

/*
BatchMap 当前交易所-市场-时间周期下，所有标的的批量执行任务池
*/
type BatchMap struct {
	Map     map[string]*BatchTask
	TFMSecs int64
	ExecMS  int64 // 执行批量任务的时间戳，每收到新的标的，推迟几秒；超过DelayBatchMS未收到，开始执行
}

type PairSub struct {
	Pair      string
	TimeFrame string
	WarmupNum int
}

type StagyJob struct {
	Stagy         *TradeStagy
	Env           *ta.BarEnv
	Entrys        []*EnterReq
	Exits         []*ExitReq
	LongOrders    []*orm.InOutOrder
	ShortOrders   []*orm.InOutOrder
	Symbol        *orm.ExSymbol     // 当前运行的币种
	TimeFrame     string            // 当前运行的时间周期
	Account       string            // 当前任务所属账号
	TPMaxs        map[int64]float64 // 订单最大盈利时价格
	OrderNum      int               // 所有未完成订单数量
	EnteredNum    int               // 已完全入场的订单数量
	CheckMS       int64             // 上次处理信号的时间戳，13位毫秒
	MaxOpenLong   int               // 最大开多数量
	MaxOpenShort  int               // 最大开空数量
	CloseLong     bool              // 是否允许平多
	CloseShort    bool              // 是否允许平空
	ExgStopLoss   bool              // 是否允许交易所止损
	LongSLPrice   float64           // 开仓时默认做多止损价格
	ShortSLPrice  float64           // 开仓时默认做空止损价格
	ExgTakeProfit bool              // 是否允许交易所止盈
	LongTPPrice   float64           // 开仓时默认做多止盈价格
	ShortTPPrice  float64           // 开仓时默认做空止盈价格
	IsWarmUp      bool              // 当前是否处于预热状态
	More          interface{}       // 策略自定义的额外信息
}

/*
EnterReq
打开一个订单。默认开多。如需开空short=False
*/
type EnterReq struct {
	Tag             string  // 入场信号
	StgyName        string  // 策略名称
	Short           bool    // 是否做空
	OrderType       int     // 订单类型, core.OrderType*
	Limit           float64 // 限价单入场价格，指定时订单将作为限价单提交
	CostRate        float64 // 开仓倍率、默认按配置1倍。用于计算LegalCost
	LegalCost       float64 // 花费法币金额。指定时忽略CostRate
	Leverage        float64 // 杠杆倍数
	Amount          float64 // 入场标的数量，由LegalCost和price计算
	StopLossVal     float64 // 入场价格到止损价格的距离，用于计算StopLoss
	StopLoss        float64 // 止损触发价格，不为空时在交易所提交一个止损单
	StopLossLimit   float64 // 止损限制价格，不提供使用StopLoss
	StopLossRate    float64 // 止损退出比例，0表示全部退出，需介于(0,1]之间
	StopLossTag     string  // 止损原因
	TakeProfitVal   float64 // 入场价格到止盈价格的距离，用于计算TakeProfit
	TakeProfit      float64 // 止盈触发价格，不为空时在交易所提交一个止盈单。
	TakeProfitLimit float64 // 止盈限制价格，不提供使用TakeProfit
	TakeProfitRate  float64 // 止盈退出比率，0表示全部退出，需介于(0,1]之间
	TakeProfitTag   string  // 止盈原因
	StopBars        int     // 入场限价单超过多少个bar未成交则取消
}

/*
ExitReq
请求平仓
*/
type ExitReq struct {
	Tag        string  // 退出信号
	StgyName   string  // 策略名称
	EnterTag   string  // 只退出入场信号为EnterTag的订单
	Dirt       int     // core.OdDirt* long/short/both
	OrderType  int     // 订单类型, core.OrderType*
	Limit      float64 // 限价单退出价格，指定时订单将作为限价单提交
	ExitRate   float64 // 退出比率，默认100%即所有订单全部退出
	Amount     float64 // 要退出的标的数量。指定时ExitRate无效
	OrderID    int64   // 只退出指定订单
	UnOpenOnly bool    // True时只退出尚未入场的订单
	FilledOnly bool    // True时只退出已入场的订单
	Force      bool    // 是否强制退出
}
