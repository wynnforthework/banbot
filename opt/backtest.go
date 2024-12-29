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
	"path/filepath"
	"runtime/pprof"
	"slices"
	"time"
)

const (
	ShowNum = 600
)

type BackTest struct {
	biz.Trader
	*BTResult
	lastDumpMs int64 // The last time the backtest status was saved 上一次保存回测状态的时间
	dp         *data.HistProvider
	isOpt      bool // whether is hyper optimization
}

var (
	nextRefresh int64 // The time of the next refresh of the trading pair 下一次刷新交易对的时间
	schedule    cron.Schedule
)

func NewBackTest(isOpt bool, outDir string) *BackTest {
	p := &BackTest{
		BTResult: NewBTResult(),
		isOpt:    isOpt,
	}
	if outDir == "" && !isOpt {
		hash, err := config.Data.HashCode()
		if err != nil {
			panic(err)
		}
		outDir = fmt.Sprintf("%s/backtest/%s", config.GetDataDir(), hash)
	}
	p.OutDir = outDir
	config.LoadPerfs(config.GetDataDir())
	biz.InitFakeWallets()
	wallets := biz.GetWallets(config.DefAcc)
	p.TotalInvest = wallets.TotalLegal(nil, false)
	p.dp = data.NewHistProvider(p.FeedKLine, p.OnEnvEnd, !isOpt)
	biz.InitLocalOrderMgr(p.orderCB, !isOpt)
	return p
}

func (b *BackTest) Init() *errs.Error {
	btime.CurTimeMS = config.TimeRange.StartMS
	b.MinReal = math.MaxFloat64
	err := orm.InitListDates()
	if err != nil {
		return err
	}
	if b.OutDir != "" {
		err_ := os.MkdirAll(b.OutDir, 0755)
		if err_ != nil {
			return errs.New(core.ErrIOWriteFail, err_)
		}
	}
	err = ormo.InitTask(!b.isOpt, b.OutDir)
	if err != nil {
		return err
	}
	err = b.initTaskOut()
	if err != nil {
		return err
	}
	// 交易对初始化
	err = biz.LoadRefreshPairs(b.dp, !b.isOpt)
	biz.InitOdSubs()
	return err
}

func (b *BackTest) FeedKLine(bar *orm.InfoKline) {
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
		return
	}
	if !bar.IsWarmUp && core.CheckWallets {
		core.CheckWallets = false
		b.logState(bar.Time, curTime)
	}
	if !core.BotRunning {
		b.dp.Terminate()
		return
	}
	if nextRefresh > 0 && bar.Time >= nextRefresh {
		// 刷新交易对
		nextRefresh = schedule.Next(time.UnixMilli(bar.Time)).UnixMilli()
		biz.AutoRefreshPairs(b.dp, !b.isOpt)
		log.Info("refreshed pairs at", zap.String("date", btime.ToDateStr(curTime, "")))
		b.dp.SetDirty()
	}
}

func (b *BackTest) Run() {
	err := b.Init()
	if err != nil {
		log.Error("backtest init fail", zap.Error(err))
		return
	}
	err = initRefreshCron()
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
	err = ormo.DumpOrdersGob(filepath.Join(b.OutDir, "orders.gob"))
	if err != nil {
		log.Warn("dump orders fail", zap.Error(err))
	}
	b.logPlot(biz.GetWallets(config.DefAcc), btime.TimeMS(), -1, -1)
	if !b.isOpt {
		log.Info(fmt.Sprintf("Complete! cost: %.1fs, avg: %.1f bar/s", btCost, float64(b.BarNum)/btCost))
		b.printBtResult()
	} else {
		b.Collect()
	}
}

func (b *BackTest) initTaskOut() *errs.Error {
	config.Args.Logfile = b.OutDir + "/out.log"
	if utils.Exists(config.Args.Logfile) {
		err_ := os.Remove(config.Args.Logfile)
		if err_ != nil {
			log.Warn("delete old log fail", zap.Error(err_))
		}
	}
	config.Args.SetLog(!b.isOpt)
	// 检查是否profile
	if config.Args.CPUProfile {
		outPath := b.OutDir + "/cpu.profile"
		if _, err_ := os.Stat(outPath); err_ == nil {
			err_ = os.Remove(outPath)
			if err_ != nil {
				log.Error("del old cpu.profile fail", zap.Error(err_))
			}
		}
		f, err_ := os.OpenFile(outPath, os.O_CREATE|os.O_RDWR, 0644)
		if err_ != nil {
			log.Error("write to cpu.profile fail", zap.Error(err_))
		} else {
			err_ = pprof.StartCPUProfile(f)
			if err_ != nil {
				log.Error("start cpu profile fail", zap.Error(err_))
			} else {
				log.Info("start profile cpu", zap.String("path", f.Name()))
			}
			core.ExitCalls = append(core.ExitCalls, func() {
				pprof.StopCPUProfile()
				err_ = f.Close()
				if err_ != nil {
					log.Error("save cpu.profile fail", zap.Error(err_))
				}
			})
		}
	}
	if config.Args.MemProfile {
		f, err_ := os.OpenFile(b.OutDir+"/mem.profile", os.O_CREATE|os.O_RDWR, 0644)
		if err_ != nil {
			log.Error("write to mem.profile fail", zap.Error(err_))
		} else {
			core.MemOut = f
			core.ExitCalls = append(core.ExitCalls, func() {
				err_ = f.Close()
				if err_ != nil {
					log.Error("save mem.profile fail", zap.Error(err_))
				}
			})
		}
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

func (b *BackTest) onLiquidation(symbol string) {
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

func (b *BackTest) orderCB(order *ormo.InOutOrder, isEnter bool) {
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

func initRefreshCron() *errs.Error {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if config.PairMgr.Cron != "" {
		var err_ error
		schedule, err_ = parser.Parse(config.PairMgr.Cron)
		if err_ != nil {
			return errs.New(core.ErrBadConfig, err_)
		}
		startTime := time.UnixMilli(config.TimeRange.StartMS)
		nextRefresh = schedule.Next(startTime).UnixMilli()
	}
	return nil
}
