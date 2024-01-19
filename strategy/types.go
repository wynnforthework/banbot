package strategy

import (
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/orm"
	ta "github.com/banbox/banta"
)

type CalcDDExitRate func(s *StagyJob, od *orm.InOutOrder, maxChg float64) float64
type PickTimeFrameFunc func(exg string, symbol string, tfScores []goods.TfScore) string

type TradeStagy struct {
	Name         string
	Version      uint16
	WarmupNum    uint16
	MinTfScore   float64
	WatchBook    bool
	DrawDownExit bool
	StakeAmount  float64
	PairInfos    []*PairSub

	OnStartUp           func(s *StagyJob)
	OnBar               func(s *StagyJob)
	OnInfoBar           func(s *StagyJob, info *PairSub)      //其他依赖的bar数据
	OnTrades            func(s *StagyJob, trades []*Trade)    // 逐笔交易数据
	OnCheckExit         func(s *StagyJob, od *orm.InOutOrder) //自定义订单退出逻辑
	GetDrawDownExitRate CalcDDExitRate                        // 计算跟踪止盈回撤退出的比率
	PickTimeFrame       PickTimeFrameFunc                     // 为指定币选择适合的交易周期
	OnShutDown          func(s *StagyJob)                     // 机器人停止时回调
}

type PairSub struct {
	Pair      string
	TimeFrame string
	WarmupNum uint16
}

type StagyJob struct {
	Stagy         *TradeStagy
	Env           *ta.BarEnv
	Entrys        []*EnterReq
	Exits         []*ExitReq
	Orders        []*orm.InOutOrder
	Symbol        *orm.ExSymbol     //当前运行的币种
	TimeFrame     string            //当前运行的时间周期
	TPMaxs        map[int64]float64 // 订单最大盈利时价格
	EnterNum      uint32            //记录已提交入场订单数量，避免访问数据库过于频繁
	CheckMS       uint32            //上次处理信号的时间戳，13位毫秒
	OpenLong      bool              // 是否允许开多
	OpenShort     bool              //是否允许开空
	CloseLong     bool              //是否允许平多
	CloseShort    bool              //是否允许平空
	ExgStopLoss   bool              // 是否允许交易所止损
	LongSLPrice   float64           //做多止损价格
	ShortSLPrice  float64           //做空止损价格
	ExgTakeProfit bool              //是否允许交易所止盈
	LongTPPrice   float64           //做多止盈价格
	ShortTPPrice  float64           //做空止盈价格
	More          interface{}       // 策略自定义的额外信息
}

type Trade struct {
}

/*
打开一个订单。默认开多。如需开空short=False
*/
type EnterReq struct {
	Tag        string  // 入场信号
	Short      bool    // 是否做空
	Limit      float64 //限价单入场价格，指定时订单将作为限价单提交
	CostRate   float64 //开仓倍率、默认按配置1倍。用于计算LegalCost
	LegalCost  float64 //花费法币金额。指定时忽略CostRate
	Leverage   int     // 杠杆倍数
	Amount     float64 //入场标的数量，由LegalCost和price计算
	StopLoss   float64 //止损价格，不为空时在交易所提交一个止损单
	TakeProfit float64 //止盈价格，不为空时在交易所提交一个止盈单。
}

/*
请求平仓
*/
type ExitReq struct {
	Tag        string  //退出信号
	Short      bool    //是否平空，默认否
	Limit      float64 //限价单退出价格，指定时订单将作为限价单提交
	ExitRate   float64 //退出比率，默认100%即所有订单全部退出
	Amount     float64 //要退出的标的数量。指定时ExitRate无效
	EnterTag   string  //只退出入场信号为EnterTag的订单
	OrderID    int64   //只退出指定订单
	UnOpenOnly bool    //True时只退出尚未入场的订单
}
