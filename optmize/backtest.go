package optmize

import (
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"math"
	"os"
)

const (
	ShowNum = 600
)

type BackTest struct {
	biz.Trader
	*BTResult
}

func NewBackTest() *BackTest {
	p := &BackTest{
		BTResult: NewBTResult(),
	}
	biz.InitFakeWallets()
	wallets := biz.GetWallets("")
	p.TotalInvest = wallets.TotalLegal(nil, false)
	data.InitHistProvider(p.FeedKLine)
	biz.InitLocalOrderMgr(p.orderCB)
	return p
}

func (b *BackTest) Init() *errs.Error {
	btime.CurTimeMS = config.TimeRange.StartMS
	b.MinReal = math.MaxFloat64
	if config.FixTFKline {
		err := orm.SyncKlineTFs()
		if err != nil {
			return err
		}
	}
	err := orm.InitTask()
	if err != nil {
		return err
	}
	taskId := orm.GetTaskID("")
	b.OutDir = fmt.Sprintf("%s/backtest/task_%d", config.GetDataDir(), taskId)
	err_ := os.MkdirAll(b.OutDir, 0755)
	if err_ != nil {
		return errs.New(core.ErrIOWriteFail, err_)
	}
	config.Args.Logfile = b.OutDir + "/out.log"
	log.Setup(config.Args.Debug, config.Args.Logfile)
	b.checkCfg()
	// 交易对初始化
	log.Info("loading exchange markets ...")
	err = orm.EnsureExgSymbols(exg.Default)
	if err != nil {
		return err
	}
	err = orm.InitListDates()
	if err != nil {
		return err
	}
	err = exg.Default.LoadLeverageBrackets(false, nil)
	if err != nil {
		return err
	}
	err = goods.RefreshPairList(nil)
	if err != nil {
		return err
	}
	var warms map[string]map[string]int
	warms, err = strategy.LoadStagyJobs(core.Pairs, core.PairTfScores)
	if err != nil {
		return err
	}
	return data.Main.SubWarmPairs(warms)
}

func (b *BackTest) FeedKLine(bar *banexg.PairTFKline) {
	b.BarNum += 1
	err := b.Trader.FeedKline(bar)
	if err != nil {
		if err.Code == core.ErrLiquidation {
			b.onLiquidation(bar.Symbol)
		} else {
			log.Error("FeedKline fail", zap.String("p", bar.Symbol), zap.Error(err))
		}
		return
	}
	if !core.IsWarmUp {
		b.logState(btime.TimeMS())
	}
}

func (b *BackTest) Run() {
	err := b.Init()
	if err != nil {
		log.Error("backtest init fail", zap.Error(err))
		return
	}
	core.PrintStagyGroups()
	btStart := btime.UTCTime()
	err = data.Main.LoopMain()
	if err != nil {
		log.Error("backtest loop fail", zap.Error(err))
		return
	}
	btCost := btime.UTCTime() - btStart
	err = biz.GetOdMgr("").CleanUp()
	if err != nil {
		log.Error("backtest clean orders fail", zap.Error(err))
		return
	}
	log.Info(fmt.Sprintf("Complete! cost: %.1fs, avg: %.1f bar/s", btCost, float64(b.BarNum)/btCost))
	b.printBtResult()
}

func (b *BackTest) checkCfg() {
	_, ok := config.Accounts[config.DefAcc]
	if !ok {
		panic("default Account invalid!")
	}
	if config.StakePct > 0 {
		log.Warn("stake_amt may result in inconsistent order amounts with each backtest!")
	}
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

func (b *BackTest) logState(timeMS int64) {
	if b.StartMS == 0 {
		b.StartMS = timeMS
	}
	b.EndMS = timeMS
	b.logPlot(timeMS)
	wallets := biz.GetWallets("")
	quoteLegal := wallets.TotalLegal(config.StakeCurrency, true)
	b.MinReal = min(b.MinReal, quoteLegal)
	if quoteLegal >= b.MaxReal {
		b.MaxReal = quoteLegal
	} else {
		drawDownPct := (b.MaxReal - quoteLegal) * 100 / b.MaxReal
		b.MaxDrawDownPct = max(b.MaxDrawDownPct, drawDownPct)
	}
}

func (b *BackTest) logPlot(timeMS int64) {
	if b.BarNum%b.PlotEvery != 0 {
		return
	}
	splStep := 5
	if len(b.Plots.Real) >= ShowNum*splStep {
		// 检查数据是否太多，超过采样总数5倍时，进行重采样
		b.PlotEvery *= splStep
		oldNum := len(b.Plots.Real)
		newNum := oldNum / splStep
		plots := PlotData{
			Labels:        make([]string, 0, newNum),
			OdNum:         make([]int, 0, newNum),
			Real:          make([]float64, 0, newNum),
			Available:     make([]float64, 0, newNum),
			UnrealizedPOL: make([]float64, 0, newNum),
			WithDraw:      make([]float64, 0, newNum),
		}
		for i := 0; i < oldNum; i += splStep {
			plots.Labels = append(plots.Labels, b.Plots.Labels[i])
			plots.OdNum = append(plots.OdNum, b.Plots.OdNum[i])
			plots.Real = append(plots.Real, b.Plots.Real[i])
			plots.Available = append(plots.Available, b.Plots.Available[i])
			plots.UnrealizedPOL = append(plots.UnrealizedPOL, b.Plots.UnrealizedPOL[i])
			plots.WithDraw = append(plots.WithDraw, b.Plots.WithDraw[i])
		}
		b.Plots = plots
		return
	}
	wallets := biz.GetWallets("")
	avaLegal := wallets.AvaLegal(nil)
	totalLegal := wallets.TotalLegal(nil, true)
	profitLegal := wallets.UnrealizedPOLLegal(nil)
	drawLegal := wallets.GetWithdrawLegal(nil)
	curDate := btime.ToDateStr(timeMS, "")
	b.Plots.Labels = append(b.Plots.Labels, curDate)
	b.Plots.OdNum = append(b.Plots.OdNum, orm.OpenNum("", orm.InOutStatusPartEnter))
	b.Plots.Real = append(b.Plots.Real, totalLegal)
	b.Plots.Available = append(b.Plots.Available, avaLegal)
	b.Plots.UnrealizedPOL = append(b.Plots.UnrealizedPOL, profitLegal)
	b.Plots.WithDraw = append(b.Plots.WithDraw, drawLegal)
}
