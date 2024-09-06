package optmize

import (
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"math"
	"os"
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
	lastDumpMs int64 // 上一次保存回测状态的时间
}

var (
	nextRefresh int64 // 下一次刷新交易对的时间
	schedule    cron.Schedule
)

func NewBackTest() *BackTest {
	p := &BackTest{
		BTResult: NewBTResult(),
	}
	core.LoadPerfs(config.GetDataDir())
	biz.InitFakeWallets()
	wallets := biz.GetWallets("")
	p.TotalInvest = wallets.TotalLegal(nil, false)
	data.InitHistProvider(p.FeedKLine, p.OnEnvEnd)
	biz.InitLocalOrderMgr(p.orderCB)
	return p
}

func (b *BackTest) Init() *errs.Error {
	btime.CurTimeMS = config.TimeRange.StartMS
	b.MinReal = math.MaxFloat64
	err := orm.InitExg(exg.Default)
	if err != nil {
		return err
	}
	err = orm.InitListDates()
	if err != nil {
		return err
	}
	err = orm.InitTask()
	if err != nil {
		return err
	}
	err = b.initTaskOut()
	if err != nil {
		return err
	}
	// 交易对初始化
	err = biz.LoadRefreshPairs()
	biz.InitOdSubs()
	return err
}

func (b *BackTest) FeedKLine(bar *orm.InfoKline) {
	b.BarNum += 1
	curTime := btime.TimeMS()
	core.CheckWallets = false
	if !core.IsWarmUp {
		if curTime > strategy.LastBatchMS {
			// 进入下一个时间帧，触发批量入场回调
			waitNum := biz.TryFireBatches(curTime)
			if waitNum > 0 {
				panic(fmt.Sprintf("batch job exec fail, wait: %v", waitNum))
			}
			strategy.LastBatchMS = curTime
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
	if !core.IsWarmUp && core.CheckWallets {
		b.logState(bar.Time, curTime)
	}
	if nextRefresh > 0 && bar.Time >= nextRefresh {
		// 刷新交易对
		nextRefresh = schedule.Next(time.UnixMilli(bar.Time)).UnixMilli()
		biz.AutoRefreshPairs()
		log.Info("refreshed pairs at", zap.String("date", btime.ToDateStr(curTime, "")))
		data.Main.SetDirty()
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
	b.cronDumpBtStatus()
	core.Cron.Start()
	btStart := btime.UTCTime()
	err = data.Main.LoopMain()
	core.Cron.Stop()
	if err != nil {
		log.Error("backtest loop fail", zap.Error(err))
		return
	}
	btCost := btime.UTCTime() - btStart
	err = biz.GetOdMgr("").CleanUp()
	strategy.ExitStagyJobs()
	if err != nil {
		log.Error("backtest clean orders fail", zap.Error(err))
		return
	}
	b.logPlot(biz.GetWallets(""), btime.TimeMS(), -1, -1)
	log.Info(fmt.Sprintf("Complete! cost: %.1fs, avg: %.1f bar/s", btCost, float64(b.BarNum)/btCost))
	b.printBtResult()
}

func (b *BackTest) initTaskOut() *errs.Error {
	taskId := orm.GetTaskID("")
	b.OutDir = fmt.Sprintf("%s/backtest/task_%d", config.GetDataDir(), taskId)
	err_ := os.MkdirAll(b.OutDir, 0755)
	if err_ != nil {
		return errs.New(core.ErrIOWriteFail, err_)
	}
	config.Args.Logfile = b.OutDir + "/out.log"
	log.Setup(config.Args.LogLevel, config.Args.Logfile)
	// 检查是否profile
	if config.Args.CPUProfile {
		outPath := b.OutDir + "/cpu.profile"
		if _, err_ = os.Stat(outPath); err_ == nil {
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
		wallets := biz.GetWallets("")
		oldVal := wallets.TotalLegal(nil, false)
		biz.InitFakeWallets(symbol)
		newVal := wallets.TotalLegal(nil, false)
		b.TotalInvest += newVal - oldVal
		log.Error(fmt.Sprintf("wallet %s BOMB at %s, reset wallet and continue..", symbol, date))
	} else {
		log.Error(fmt.Sprintf("wallet %s BOMB at %s, exit", symbol, date))
		core.StopAll()
		core.BotRunning = false
	}
}

func (b *BackTest) orderCB(order *orm.InOutOrder, isEnter bool) {
	if isEnter {
		openNum := orm.OpenNum("", orm.InOutStatusPartEnter)
		if openNum > b.MaxOpenOrders {
			b.MaxOpenOrders = openNum
		}
	} else {
		wallets := biz.GetWallets("")
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
	wallets := biz.GetWallets("")
	totalLegal := wallets.TotalLegal(nil, true)
	b.MinReal = min(b.MinReal, totalLegal)
	if totalLegal >= b.MaxReal {
		b.MaxReal = totalLegal
	} else {
		drawDownPct := (b.MaxReal - totalLegal) * 100 / b.MaxReal
		b.MaxDrawDownPct = max(b.MaxDrawDownPct, drawDownPct)
	}
	odNum := orm.OpenNum("", orm.InOutStatusPartEnter)
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
		// 检查数据是否太多，超过采样总数5倍时，进行重采样
		b.PlotEvery *= splStep
		oldNum := len(b.Plots.Real)
		newNum := oldNum / splStep
		plots := &PlotData{
			Labels:        make([]string, 0, newNum),
			OdNum:         make([]int, 0, newNum),
			Real:          make([]float64, 0, newNum),
			Available:     make([]float64, 0, newNum),
			UnrealizedPOL: make([]float64, 0, newNum),
			WithDraw:      make([]float64, 0, newNum),
		}
		for i := 0; i < oldNum; i += splStep {
			plots.Labels = append(plots.Labels, b.Plots.Labels[i])
			plots.OdNum = append(plots.OdNum, slices.Max(b.Plots.OdNum[i:i+splStep]))
			plots.Real = append(plots.Real, b.Plots.Real[i])
			plots.Available = append(plots.Available, b.Plots.Available[i])
			plots.UnrealizedPOL = append(plots.UnrealizedPOL, b.Plots.UnrealizedPOL[i])
			plots.WithDraw = append(plots.WithDraw, b.Plots.WithDraw[i])
		}
		b.Plots = plots
		return
	}
	b.logPlot(wallets, timeMS, odNum, totalLegal)
}

func (b *BackTest) logPlot(wallets *biz.BanWallets, timeMS int64, odNum int, totalLegal float64) {
	if odNum < 0 {
		odNum = orm.OpenNum("", orm.InOutStatusPartEnter)
	}
	if totalLegal < 0 {
		totalLegal = wallets.TotalLegal(nil, true)
	}
	avaLegal := wallets.AvaLegal(nil)
	profitLegal := wallets.UnrealizedPOLLegal(nil)
	drawLegal := wallets.GetWithdrawLegal(nil)
	curDate := btime.ToDateStr(timeMS, "")
	b.Plots.Labels = append(b.Plots.Labels, curDate)
	b.Plots.OdNum = append(b.Plots.OdNum, odNum)
	b.Plots.Real = append(b.Plots.Real, totalLegal)
	b.Plots.Available = append(b.Plots.Available, avaLegal)
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
