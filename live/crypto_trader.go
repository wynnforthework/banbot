package live

import (
	"fmt"
	"github.com/banbox/banbot/opt"
	"github.com/banbox/banbot/orm/ormo"
	"time"

	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/rpc"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banbot/web"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

type CryptoTrader struct {
	biz.Trader
	dp *data.LiveProvider
}

func NewCryptoTrader() *CryptoTrader {
	return &CryptoTrader{}
}

func (t *CryptoTrader) Init() *errs.Error {
	config.LoadPerfs(config.GetDataDir())
	dp, err := data.NewLiveProvider(t.FeedKLine, t.OnEnvEnd)
	if err != nil {
		return err
	}
	t.dp = dp
	err = ormo.InitTask(true, config.GetDataDir())
	if err != nil {
		return err
	}
	// Trading pair initialization
	// 交易对初始化
	err = orm.InitListDates()
	if err != nil {
		return err
	}
	err = rpc.InitRPC()
	if err != nil {
		return err
	}
	err = web.StartApi()
	if err != nil {
		return err
	}
	// Order Manager initialization
	// 订单管理器初始化
	err = t.initOdMgr()
	if err != nil {
		return err
	}
	err = opt.RefreshPairJobs(dp, true, true, nil)
	lastRefreshMS = btime.TimeMS()
	// add exit callback
	core.ExitCalls = append(core.ExitCalls, exitCleanUp)
	return err
}

func (t *CryptoTrader) initOdMgr() *errs.Error {
	if !core.EnvReal {
		biz.InitLocalOrderMgr(t.orderCB, true)
		return nil
	}
	biz.InitLiveOrderMgr(t.orderCB)
	for account := range config.Accounts {
		odMgr := biz.GetLiveOdMgr(account)
		oldList, newList, delList, err := odMgr.SyncExgOrders()
		if err != nil {
			return err
		}
		openOds, lock := ormo.GetOpenODs(account)
		lock.Lock()
		msg := fmt.Sprintf("orders: %d restored, %d deleted, %d added, %d opened", len(oldList), len(delList), len(newList), len(openOds))
		lock.Unlock()
		rpc.SendMsg(map[string]interface{}{
			"type":    rpc.MsgTypeStatus,
			"account": account,
			"status":  msg,
		})
	}
	return nil
}

func (t *CryptoTrader) Run() *errs.Error {
	err := t.Init()
	if err != nil {
		return err
	}
	t.startJobs()
	err = t.dp.LoopMain()
	if err != nil {
		return err
	}
	// clean CallBacks already to core.ExitCalls
	return nil
}

func (t *CryptoTrader) FeedKLine(bar *orm.InfoKline) {
	err := t.Trader.FeedKline(bar)
	if err != nil {
		log.Error("handle bar fail", zap.String("pair", bar.Symbol), zap.Error(err))
		return
	}
	delayExecBatch()
}

func delayExecBatch() {
	time.AfterFunc(time.Millisecond*core.DelayBatchMS, func() {
		waitNum := biz.TryFireBatches(btime.UTCStamp())
		if waitNum > 0 {
			// There are TF cycles that have not yet been completed, and they are postponed for a few seconds to trigger again
			// 有尚未完成的tf周期，推迟几秒再次触发
			delayExecBatch()
		}
	})
}

func (t *CryptoTrader) orderCB(od *ormo.InOutOrder, isEnter bool) {
	sendOrderMsg(od, isEnter)
}

func (t *CryptoTrader) startJobs() {
	// Listen to the balance
	// 监听余额
	biz.WatchLiveBalances()
	// Listen to account order flow, process user orders, and consume order queues
	// 监听账户订单流、处理用户下单、消费订单队列
	biz.StartLiveOdMgr()
	// Refresh trading pairs regularly
	// 定期刷新交易对
	CronRefreshPairs(t.dp)
	// Refresh the market regularly
	// 定时刷新市场行情
	CronLoadMarkets()
	// Check every 5 minutes to see if the global stop loss is triggered
	// 每5分钟检查是否触发全局止损
	CronFatalLossCheck()
	// Regularly check the candlestick timeout, updated every minute
	// 定期检查K线超时，每分钟更新
	CronKlineDelays()
	// The timer output is executed every 5 minutes: 01:30 06:30 11:30
	// 定时输出收到K线情况，每5分钟执行：01:30  06:30  11:30
	CronKlineSummary()
	// Check every 15th minute to see if the limit order submission is triggered
	// 每分钟第15s检查是否触发限价单提交
	CronCheckTriggerOds()
	core.Cron.Start()
}

func exitCleanUp() {
	err := biz.CleanUpOdMgr()
	if err != nil {
		log.Error("clean odMgr fail", zap.Error(err))
	}
	strat.ExitStratJobs()
	core.Cron.Stop()
	err = exg.Default.Close()
	if err != nil {
		log.Error("close exg fail", zap.Error(err))
	}
	for account := range config.Accounts {
		openOds, lock := ormo.GetOpenODs(account)
		lock.Lock()
		openNum := len(openOds)
		lock.Unlock()
		msg := fmt.Sprintf("bot stop, %d orders opened", openNum)
		rpc.SendMsg(map[string]interface{}{
			"type":    rpc.MsgTypeStatus,
			"account": account,
			"status":  msg,
		})
	}
	rpc.CleanUp()
}
