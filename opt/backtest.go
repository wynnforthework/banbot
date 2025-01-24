package opt

import (
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/orm/ormo"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"math"
	"os"
	"slices"
	"time"
)

const (
	ShowNum = 600
)

type BackTestLite struct {
	biz.Trader
	*BTResult
	dp    *data.HistProvider
	isOpt bool // whether is hyper optimization
}

type BackTest struct {
	*BackTestLite
	lastDumpMs  int64 // The last time the backtest status was saved 上一次保存回测状态的时间
	PBar        *utils.StagedPrg
	nextRefresh int64 // The time of the next refresh of the trading pair 下一次刷新交易对的时间
	schedule    cron.Schedule
}

/*
NewBackTestLite 创建一个临时内部回测，仅用于寻找回测未平仓订单来接力
Create a temporary internal backtest, solely for the purpose of finding backtest open orders to relay.
*/
func NewBackTestLite(isOpt bool, onBar data.FnPairKline, pBar *utils.StagedPrg) *BackTestLite {
	b := &BackTestLite{
		BTResult: NewBTResult(),
		isOpt:    isOpt,
	}
	biz.InitFakeWallets()
	wallets := biz.GetWallets(config.DefAcc)
	b.TotalInvest = wallets.TotalLegal(nil, false)
	if onBar == nil {
		onBar = func(bar *orm.InfoKline) {
			b.FeedKLine(bar)
		}
	}
	b.dp = data.NewHistProvider(onBar, b.OnEnvEnd, !isOpt, pBar)
	biz.InitLocalOrderMgr(b.orderCB, !isOpt)
	return b
}

func (b *BackTestLite) FeedKLine(bar *orm.InfoKline) bool {
	b.BarNum += 1
	curTime := btime.TimeMS()
	if !bar.IsWarmUp {
		if curTime > strat.LastBatchMS {
			// Enter the next timeframe and trigger the batch entry callback
			// 进入下一个时间帧，触发批量入场回调
			waitNum := biz.TryFireBatches(curTime)
			if waitNum > 0 {
				panic(fmt.Sprintf("batch job exec fail, wait: %v", waitNum))
			}
			strat.LastBatchMS = curTime
		}
		if curTime > b.lastTime {
			b.lastTime = curTime
			b.TimeNum += 1
			core.CheckWallets = true
		}
	}
	err := b.Trader.FeedKline(bar)
	if err != nil {
		if err.Code == core.ErrLiquidation {
			b.onLiquidation(bar.Symbol)
		} else {
			log.Error("FeedKline fail", zap.String("p", bar.Symbol), zap.Error(err))
		}
		return false
	}
	if !core.BotRunning {
		b.dp.Terminate()
		return false
	}
	return true
}

func (b *BackTestLite) onLiquidation(symbol string) {
	date := btime.ToDateStr(btime.TimeMS(), "")
	if config.ChargeOnBomb {
		wallets := biz.GetWallets(config.DefAcc)
		oldVal := wallets.TotalLegal(nil, false)
		biz.InitFakeWallets(symbol)
		newVal := wallets.TotalLegal(nil, false)
		b.TotalInvest += newVal - oldVal
		log.Warn(fmt.Sprintf("wallet %s BOMB at %s, reset wallet and continue..", symbol, date))
	} else {
		log.Warn(fmt.Sprintf("wallet %s BOMB at %s, exit", symbol, date))
		core.StopAll()
		b.dp.Terminate()
	}
}

func (b *BackTestLite) orderCB(order *ormo.InOutOrder, isEnter bool) {
	if isEnter {
		openNum := ormo.OpenNum(config.DefAcc, ormo.InOutStatusPartEnter)
		if openNum > b.MaxOpenOrders {
			b.MaxOpenOrders = openNum
		}
	} else {
		wallets := biz.GetWallets(config.DefAcc)
		// 更新单笔开单金额
		wallets.TryUpdateStakePctAmt()
		if config.DrawBalanceOver > 0 {
			quoteLegal := wallets.AvaLegal(config.StakeCurrency)
			if quoteLegal > config.DrawBalanceOver {
				wallets.WithdrawLegal(quoteLegal-config.DrawBalanceOver, config.StakeCurrency)
			}
		}
	}
}

func NewBackTest(isOpt bool, outDir string) *BackTest {
	stages := []string{"init", "listMs", "loadPairs", "tfScores", "loadJobs", "warmJobs", "downKline", "runBT"}
	stgWeis := []float64{1, 1, 2, 2, 1, 2, 10, 10}
	b := &BackTest{
		PBar: utils.NewStagedPrg(stages, stgWeis),
	}
	b.BackTestLite = NewBackTestLite(isOpt, b.FeedKLine, b.PBar)
	if outDir == "" && !isOpt {
		hash, err := config.Data.HashCode()
		if err != nil {
			panic(err)
		}
		outDir = fmt.Sprintf("%s/backtest/%s", config.GetDataDir(), hash)
	}
	b.OutDir = config.ParsePath(outDir)
	config.LoadPerfs(config.GetDataDir())
	return b
}

func (b *BackTest) Init() *errs.Error {
	btime.CurTimeMS = config.TimeRange.StartMS
	b.MinReal = math.MaxFloat64
	if b.OutDir != "" {
		err_ := os.MkdirAll(b.OutDir, 0755)
		if err_ != nil {
			return errs.New(core.ErrIOWriteFail, err_)
		}
	}
	err := ormo.InitTask(!b.isOpt, b.OutDir)
	if err != nil {
		return err
	}
	err = b.initTaskOut()
	if err != nil {
		return err
	}
	b.PBar.SetProgress("init", 1)
	err = orm.InitListDates()
	if err != nil {
		return err
	}
	b.PBar.SetProgress("listMs", 1)
	// 交易对初始化
	err = RefreshPairJobs(b.dp, !b.isOpt, true, b.PBar)
	return err
}

func (b *BackTest) FeedKLine(bar *orm.InfoKline) {
	curTime := btime.TimeMS()
	ok := b.BackTestLite.FeedKLine(bar)
	if !bar.IsWarmUp && core.CheckWallets {
		core.CheckWallets = false
		b.logState(bar.Time, curTime)
	}
	if ok && b.nextRefresh > 0 && bar.Time >= b.nextRefresh {
		// 刷新交易对
		b.nextRefresh = b.schedule.Next(time.UnixMilli(bar.Time)).UnixMilli()
		err := RefreshPairJobs(b.dp, !b.isOpt, false, nil)
		dateStr := btime.ToDateStr(curTime, "")
		if err != nil {
			log.Error("RefreshPairJobs", zap.String("date", dateStr), zap.Error(err))
		} else {
			log.Info("refreshed pairs at", zap.String("date", dateStr))
		}
		b.dp.SetDirty()
	}
}

func (b *BackTest) Run() {
	err := b.Init()
	if err != nil {
		log.Error("backtest init fail", zap.Error(err))
		return
	}
	err = b.initRefreshCron()
	if err != nil {
		log.Error("init pair cron fail", zap.Error(err))
		return
	}
	if !b.isOpt {
		b.cronDumpBtStatus()
		core.Cron.Start()
	}
	btStart := btime.UTCTime()
	err = b.dp.LoopMain()
	if !b.isOpt {
		core.Cron.Stop()
	}
	if err != nil {
		log.Error("backtest loop fail", zap.Error(err))
		return
	}
	btCost := btime.UTCTime() - btStart
	err = biz.GetOdMgr(config.DefAcc).CleanUp()
	strat.ExitStratJobs()
	if err != nil {
		log.Error("backtest clean orders fail", zap.Error(err))
		return
	}
	b.logPlot(biz.GetWallets(config.DefAcc), btime.TimeMS(), -1, -1)
	if !b.isOpt {
		log.Info(fmt.Sprintf("Complete! cost: %.1fs, avg: %.1f bar/s", btCost, float64(b.BarNum)/btCost))
		failOpens := strat.DumpAccFailOpens()
		if failOpens != "" {
			log.Info("fail open tag nums:\n" + failOpens)
		}
		b.printBtResult()
	} else {
		b.Collect()
	}
}

func (b *BackTest) initTaskOut() *errs.Error {
	if b.OutDir != "" {
		config.Args.Logfile = b.OutDir + "/out.log"
		if utils.Exists(config.Args.Logfile) {
			err_ := os.Remove(config.Args.Logfile)
			if err_ != nil {
				log.Warn("delete old log fail", zap.Error(err_))
			}
		}
		config.Args.SetLog(!b.isOpt)
	}
	_, ok := config.Accounts[config.DefAcc]
	if !ok {
		panic("default Account invalid!")
	}
	if config.StakePct > 0 {
		log.Warn("stake_amt may result in inconsistent order amounts with each backtest!")
	}
	return nil
}

func (b *BackTest) logState(startMS, timeMS int64) {
	if b.StartMS == 0 {
		b.StartMS = startMS
	}
	b.EndMS = timeMS
	wallets := biz.GetWallets(config.DefAcc)
	totalLegal := wallets.TotalLegal(nil, true)
	b.MinReal = min(b.MinReal, totalLegal)
	if totalLegal >= b.MaxReal {
		b.MaxReal = totalLegal
	} else {
		drawDownPct := (b.MaxReal - totalLegal) * 100 / b.MaxReal
		b.MaxDrawDownPct = max(b.MaxDrawDownPct, drawDownPct)
		b.MaxDrawDownVal = max(b.MaxDrawDownVal, b.MaxReal-totalLegal)
	}
	odNum := ormo.OpenNum(config.DefAcc, ormo.InOutStatusPartEnter)
	if b.TimeNum%b.PlotEvery != 0 {
		if odNum > b.Plots.tmpOdNum {
			b.Plots.tmpOdNum = odNum
		}
		return
	}
	if b.Plots.tmpOdNum > odNum {
		odNum = b.Plots.tmpOdNum
	}
	b.Plots.tmpOdNum = 0
	splStep := 5
	if len(b.Plots.Real) >= ShowNum*splStep {
		// Check whether there is too much data and resample if the total number of samples exceeds 5 times
		// 检查数据是否太多，超过采样总数5倍时，进行重采样
		b.PlotEvery *= splStep
		oldNum := len(b.Plots.Real)
		newNum := oldNum / splStep
		plots := &PlotData{
			Labels:        make([]string, 0, newNum),
			OdNum:         make([]int, 0, newNum),
			JobNum:        make([]int, 0, newNum),
			Real:          make([]float64, 0, newNum),
			Available:     make([]float64, 0, newNum),
			UnrealizedPOL: make([]float64, 0, newNum),
			WithDraw:      make([]float64, 0, newNum),
		}
		for i := 0; i < oldNum; i += splStep {
			plots.Labels = append(plots.Labels, b.Plots.Labels[i])
			plots.OdNum = append(plots.OdNum, slices.Max(b.Plots.OdNum[i:i+splStep]))
			plots.JobNum = append(plots.JobNum, slices.Max(b.Plots.JobNum[i:i+splStep]))
			plots.Real = append(plots.Real, b.Plots.Real[i])
			plots.Available = append(plots.Available, b.Plots.Available[i])
			plots.Profit = append(plots.Profit, b.Plots.Profit[i])
			plots.UnrealizedPOL = append(plots.UnrealizedPOL, b.Plots.UnrealizedPOL[i])
			plots.WithDraw = append(plots.WithDraw, b.Plots.WithDraw[i])
		}
		b.Plots = plots
		return
	}
	// 这里应使用bar的开始时间，避免多个时间周期运行时，大部分CheckMS未更新
	b.logPlot(wallets, startMS, odNum, totalLegal)
}

func (b *BackTest) logPlot(wallets *biz.BanWallets, timeMS int64, odNum int, totalLegal float64) {
	if odNum < 0 {
		odNum = ormo.OpenNum(config.DefAcc, ormo.InOutStatusPartEnter)
	}
	jobNum := 0
	jobMap := strat.GetJobs(wallets.Account)
	for _, jobs := range jobMap {
		for _, j := range jobs {
			if j.CheckMS+j.Env.TFMSecs >= timeMS {
				jobNum += 1
			}
		}
	}
	if totalLegal < 0 {
		totalLegal = wallets.TotalLegal(nil, true)
	}
	avaLegal := wallets.AvaLegal(nil)
	profitLegal := wallets.UnrealizedPOLLegal(nil)
	drawLegal := wallets.GetWithdrawLegal(nil)
	curDate := btime.ToDateStr(timeMS, "")
	b.donePftLegal += ormo.LegalDoneProfits(b.histOdOff)
	b.histOdOff = len(ormo.HistODs)
	b.Plots.Labels = append(b.Plots.Labels, curDate)
	b.Plots.OdNum = append(b.Plots.OdNum, odNum)
	b.Plots.JobNum = append(b.Plots.JobNum, jobNum)
	b.Plots.Real = append(b.Plots.Real, totalLegal)
	b.Plots.Available = append(b.Plots.Available, avaLegal)
	b.Plots.Profit = append(b.Plots.Profit, b.donePftLegal)
	b.Plots.UnrealizedPOL = append(b.Plots.UnrealizedPOL, profitLegal)
	b.Plots.WithDraw = append(b.Plots.WithDraw, drawLegal)
}

func (b *BackTest) cronDumpBtStatus() {
	b.lastDumpMs = btime.UTCStamp()
	_, err_ := core.Cron.AddFunc("30 * * * * *", func() {
		curTime := btime.UTCStamp()
		if curTime-b.lastDumpMs < 300000 {
			// 5分钟保存一次回测状态
			return
		}
		b.lastDumpMs = curTime
		log.Info("dump backTest status to files...")
		b.printBtResult()
	})
	if err_ != nil {
		log.Error("add Dump BackTest Status fail", zap.Error(err_))
	}
}

func (b *BackTest) initRefreshCron() *errs.Error {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if config.PairMgr.Cron != "" {
		var err_ error
		b.schedule, err_ = parser.Parse(config.PairMgr.Cron)
		if err_ != nil {
			return errs.New(core.ErrBadConfig, err_)
		}
		baseMS := config.TimeRange.StartMS
		for {
			baseTime := time.UnixMilli(baseMS)
			b.nextRefresh = b.schedule.Next(baseTime).UnixMilli()
			if b.nextRefresh-baseMS > config.MinPairCronGapMS {
				break
			}
			baseMS = b.nextRefresh
		}
	}
	return nil
}

func RefreshPairJobs(dp data.IProvider, showLog, isFirst bool, pBar *utils.StagedPrg) *errs.Error {
	pairs, pairTfScores, err := biz.RefreshPairs(showLog, pBar)
	if err != nil {
		return err
	}
	// store the currently running jobs and mark them as prohibited from running
	// 获取旧的已运行一段时间的任务（在刷新任务前运行），标记为禁止运行
	forbidJobs := strat.GetJobKeys()
	// 刷新交易任务
	warms, err := biz.RefreshJobs(pairs, pairTfScores, showLog, pBar)
	if err != nil {
		return err
	}
	if isFirst {
		// 监听订单状态变化，触发策略的OnOrderChange
		biz.InitOdSubs()
	}
	// relay the simulate open position orders for new symbols at this time
	// 接力入场新品种的截止此时模拟持仓订单
	err = relayUnFinishOrders(pairTfScores, forbidJobs, isFirst)
	if err != nil {
		return err
	}
	// warm up for new symbols
	return dp.SubWarmPairs(warms, true)
}

/*
获取模拟回测的未完成订单，接力入场；
应在RefreshJobs之后再调用，否则入场订单可能被视为旧的平仓掉
*/
func relayUnFinishOrders(pairTfScores map[string]map[string]float64, forbidJobs map[string]map[string]bool, isFirst bool) *errs.Error {
	if !config.RelaySimUnFinish {
		return nil
	}
	// Backup global state
	// 备份全局状态
	backUp := biz.BackupVars()
	timeRange := &config.TimeTuple{
		StartMS: config.TimeRange.StartMS,
		EndMS:   config.TimeRange.EndMS,
	}
	backTime := btime.CurTimeMS
	backPols := config.RunPolicy
	// Divide into multiple groups based on the subscription period according to the strategy
	// 按策略订阅周期划分为多个组
	groups := strat.RelayPolicyGroups()
	// pair_tf_stratID
	var relayOpens = make(map[string]*ormo.InOutOrder)
	var relayDones = make(map[string]*ormo.InOutOrder)
	for _, gp := range groups {
		// Reset global variables, backtest for time range, and search for open orders
		// 重置全局变量，回测过去一段时间，查找未平仓订单
		biz.ResetVars()
		strat.ForbidJobs = forbidJobs
		btime.CurTimeMS = gp.StartMS
		config.TimeRange = &config.TimeTuple{
			StartMS: gp.StartMS,
			EndMS:   backTime,
		}
		lite := NewBackTestLite(true, nil, nil)
		// set policy to run
		// 重新加载策略任务
		config.RunPolicy = gp.Policies
		warms, _, err := strat.LoadStratJobs(core.Pairs, pairTfScores)
		if err != nil {
			return err
		}
		if len(warms) == 0 {
			// 没有需要预回测的任务
			continue
		}
		err = lite.dp.SubWarmPairs(warms, true)
		if err != nil {
			return err
		}
		err = lite.dp.LoopMain()
		if err != nil {
			return err
		}
		// Record the last unfinished orders
		// 记录最后的未完成订单
		odMap, lock := ormo.GetOpenODs(config.DefAcc)
		lock.Lock()
		for _, od := range odMap {
			if od.Status >= ormo.InOutStatusPartEnter {
				relayOpens[od.KeyAlign()] = od
			}
		}
		lock.Unlock()
		for _, od := range ormo.HistODs {
			relayDones[od.KeyAlign()] = od
		}
	}
	config.RunPolicy = backPols
	config.TimeRange = timeRange
	btime.CurTimeMS = backTime
	// Restore global variables and relay open orders for entry
	// 恢复全局变量，接力入场未平仓订单
	biz.RestoreVars(backUp)
	if isFirst {
		// 如果是初次执行，检查打开的订单是否已在测试期间平仓，是则自动平仓
		// 主要针对实盘隔一段时间后重启有未平仓订单场景，需检查订单是否应在机器人停止期间平仓
		var sess *ormo.Queries
		var err *errs.Error
		if core.LiveMode {
			sess, err = ormo.Conn(orm.DbTrades, true)
			if err != nil {
				return err
			}
		}
		closeNums := make(map[string]int)
		for acc := range config.Accounts {
			odMgr := biz.GetOdMgr(acc)
			odMap, lock := ormo.GetOpenODs(acc)
			var exitOds []*ormo.InOutOrder
			lock.Lock()
			for _, od := range odMap {
				if _, ok := relayDones[od.KeyAlign()]; ok {
					exitOds = append(exitOds, od)
				}
			}
			lock.Unlock()
			if len(exitOds) > 0 {
				err = odMgr.ExitAndFill(sess, exitOds, &strat.ExitReq{Tag: core.ExitTagExitDelay})
				if err != nil {
					log.Error("close delayed order fail", zap.Int("num", len(exitOds)), zap.Error(err))
				} else {
					closeNums[acc] = len(exitOds)
				}
			}
		}
		if len(closeNums) > 0 {
			log.Info("closed delayed order", zap.Any("nums", closeNums))
		}
	}
	if len(relayOpens) == 0 {
		return nil
	}
	var sess *ormo.Queries
	var err *errs.Error
	if core.LiveMode {
		sess, err = ormo.Conn(orm.DbTrades, true)
		if err != nil {
			return err
		}
	}
	openOds := utils.ValsOfMap(relayOpens)
	for acc := range config.Accounts {
		odMgr := biz.GetOdMgr(acc)
		err = odMgr.RelayOrders(sess, openOds)
		if err != nil {
			return err
		}
		jobs := strat.GetJobs(acc)
		for _, od := range openOds {
			stgMap, ok := jobs[fmt.Sprintf("%s_%s", od.Symbol, od.Timeframe)]
			if ok {
				if job, ok := stgMap[od.Strategy]; ok {
					job.OrderNum += 1
					continue
				}
			}
			log.Warn("job not found for", zap.String("key", od.Key()))
		}
	}
	return nil
}
