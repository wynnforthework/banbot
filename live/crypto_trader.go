package live

import (
	"fmt"
	"github.com/banbox/banbot/api"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/rpc"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"os"
	"time"
)

type CryptoTrader struct {
	biz.Trader
	dp *data.LiveProvider
}

func NewCryptoTrader() *CryptoTrader {
	return &CryptoTrader{}
}

func (t *CryptoTrader) Init() *errs.Error {
	core.LoadPerfs(config.GetDataDir())
	dp, err := data.NewLiveProvider(t.FeedKLine, t.OnEnvEnd)
	if err != nil {
		return err
	}
	t.dp = dp
	err = orm.InitTask()
	if err != nil {
		return err
	}
	if config.Args.Logfile != "" {
		outDir := fmt.Sprintf("%s/live", config.GetDataDir())
		err_ := os.MkdirAll(outDir, 0755)
		if err_ != nil {
			return errs.New(core.ErrIOWriteFail, err_)
		}
		config.Args.Logfile = outDir + "/out.log"
	}
	log.Setup(config.Args.LogLevel, config.Args.Logfile)
	// Trading pair initialization
	// 交易对初始化
	log.Info("loading exchange markets ...")
	err = orm.InitExg(exg.Default)
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
	err = api.StartApi()
	if err != nil {
		return err
	}
	// Order Manager initialization
	// 订单管理器初始化
	err = t.initOdMgr()
	if err != nil {
		return err
	}
	err = biz.LoadRefreshPairs(dp)
	biz.InitOdSubs()
	return err
}

func (t *CryptoTrader) initOdMgr() *errs.Error {
	if !core.EnvReal {
		biz.InitLocalOrderMgr(t.orderCB)
		return nil
	}
	biz.InitLiveOrderMgr(t.orderCB)
	for account := range config.Accounts {
		odMgr := biz.GetLiveOdMgr(account)
		oldList, newList, delList, err := odMgr.SyncExgOrders()
		if err != nil {
			return err
		}
		openOds, lock := orm.GetOpenODs(account)
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
	err = biz.CleanUpOdMgr()
	strat.ExitStagyJobs()
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

func (t *CryptoTrader) orderCB(od *orm.InOutOrder, isEnter bool) {
	msgType := rpc.MsgTypeExit
	subOd := od.Exit
	action := "Close Long"
	if od.Short {
		action = "Close Short"
	}
	if isEnter {
		msgType = rpc.MsgTypeEntry
		subOd = od.Enter
		action = "Open Long"
		if od.Short {
			action = "Open Short"
		}
	}
	if subOd.Status != orm.OdStatusClosed || subOd.Amount == 0 {
		return
	}
	account := orm.GetTaskAcc(od.TaskID)
	rpc.SendMsg(map[string]interface{}{
		"type":          msgType,
		"account":       account,
		"action":        action,
		"enter_tag":     od.EnterTag,
		"exit_tag":      od.ExitTag,
		"side":          subOd.Side,
		"short":         od.Short,
		"leverage":      od.Leverage,
		"amount":        subOd.Amount,
		"price":         subOd.Price,
		"value":         subOd.Amount * subOd.Price,
		"cost":          subOd.Amount * subOd.Price / od.Leverage,
		"strategy":      od.Strategy,
		"pair":          od.Symbol,
		"timeframe":     od.Timeframe,
		"profit":        od.Profit,
		"profit_rate":   od.ProfitRate,
		"max_draw_down": od.MaxDrawDown,
	})
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
