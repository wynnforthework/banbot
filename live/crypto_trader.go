package live

import (
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/rpc"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

type CryptoTrader struct {
	biz.Trader
}

func NewCryptoTrader() *CryptoTrader {
	res := &CryptoTrader{}
	biz.InitLiveOrderMgr(res.orderCB)
	return res
}

func (t *CryptoTrader) Init() *errs.Error {
	err := orm.SyncKlineTFs()
	if err != nil {
		return err
	}
	err = data.InitLiveProvider(t.FeedKLine)
	if err != nil {
		return err
	}
	biz.InitLiveWallets()
	err = orm.InitTask()
	if err != nil {
		return err
	}
	// 交易对初始化
	err = orm.EnsureExgSymbols(exg.Default)
	if err != nil {
		return err
	}
	err = orm.InitListDates()
	if err != nil {
		return err
	}
	err = rpc.InitRPC()
	if err != nil {
		return err
	}
	// 先更新所有未平仓订单的状态
	oldList, newList, delList, err := biz.OdMgrLive.SyncExgOrders()
	if err != nil {
		return err
	}
	var addPairs = make(map[string]bool)
	for _, od := range orm.OpenODs {
		addPairs[od.Symbol] = true
	}
	err = goods.RefreshPairList(utils.KeysOfMap(addPairs))
	if err != nil {
		return err
	}
	var warms map[string]map[string]int
	warms, err = strategy.LoadStagyJobs(core.Pairs, core.PairTfScores)
	if err != nil {
		return err
	}
	err = data.Main.SubWarmPairs(warms)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("订单恢复%d，删除%d，新增%d，已开启%d单", len(oldList), len(delList), len(newList), len(orm.OpenODs))
	rpc.SendMsg(map[string]interface{}{
		"type":   rpc.MsgTypeStatus,
		"status": msg,
	})
	return nil
}

func (t *CryptoTrader) Run() *errs.Error {
	err := t.Init()
	if err != nil {
		return err
	}
	core.PrintStagyGroups()
	t.startJobs()
	err = data.Main.LoopMain()
	if err != nil {
		return err
	}
	err = biz.OdMgrLive.CleanUp()
	if err != nil {
		return err
	}
	core.Cron.Stop()
	err = exg.Default.Close()
	if err != nil {
		return err
	}
	rpc.CleanUp()
	return nil
}

func (t *CryptoTrader) FeedKLine(bar *banexg.PairTFKline) {
	err := t.Trader.FeedKline(bar)
	if err != nil {
		log.Error("handle bar fail", zap.String("pair", bar.Symbol), zap.Error(err))
	}
}

func (t *CryptoTrader) orderCB(od *orm.InOutOrder, isEnter bool) {
	msgType := rpc.MsgTypeExit
	subOd := od.Exit
	action := "平多"
	if od.Short {
		action = "平空"
	}
	if isEnter {
		msgType = rpc.MsgTypeEntry
		subOd = od.Enter
		action = "开多"
		if od.Short {
			action = "开空"
		}
	}
	if subOd.Status != orm.OdStatusClosed || subOd.Amount == 0 {
		return
	}
	rpc.SendMsg(map[string]interface{}{
		"type":        msgType,
		"action":      action,
		"enter_tag":   od.EnterTag,
		"exit_tag":    od.ExitTag,
		"side":        subOd.Side,
		"short":       od.Short,
		"leverage":    od.Leverage,
		"amount":      subOd.Amount,
		"price":       subOd.Price,
		"value":       subOd.Amount * subOd.Price,
		"cost":        subOd.Amount * subOd.Price / float64(od.Leverage),
		"strategy":    od.Strategy,
		"pair":        od.Symbol,
		"timeframe":   od.Timeframe,
		"profit":      od.Profit,
		"profit_rate": od.ProfitRate,
	})
}

func (t *CryptoTrader) startJobs() {
	// 监听余额
	biz.WatchLiveBalances()
	// 监听杠杆倍数
	biz.WatchLeverages()
	// 监听账户订单流
	biz.OdMgrLive.WatchMyTrades()
	// 跟踪用户下单
	biz.OdMgrLive.TrialUnMatchesForever()
	// 消费订单队列
	biz.OdMgrLive.ConsumeOrderQueue()
	// 定期刷新交易对
	CronRefreshPairs()
	// 定时刷新市场行情
	CronLoadMarkets()
	// 每5分钟检查是否触发全局止损
	CronFatalLossCheck()
	// 定期检查K线超时，每分钟更新
	CronKlineDelays()
	// 定时输出收到K线情况，每5分钟执行：01:30  06:30  11:30
	CronKlineSummary()
	// 每分钟第15s检查是否触发限价单提交
	CronCheckTriggerOds()
	core.Cron.Start()
}
