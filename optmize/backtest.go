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
	p.TotalInvest = biz.Wallets.TotalLegal(nil, false)
	data.InitHistProvider(p.FeedKLine)
	biz.InitLocalOrderMgr(p.orderCB)
	return p
}

func (b *BackTest) Init() *errs.Error {
	core.RunMode = core.RunModeBackTest
	btime.CurTimeMS = config.TimeRange.StartMS
	b.MinBalance = math.MaxFloat64
	err := orm.SyncKlineTFs()
	if err != nil {
		return err
	}
	err = orm.InitTask()
	if err != nil {
		return err
	}
	b.OutDir = fmt.Sprintf("%s/backtest/task_%d", config.GetDataDir(), orm.TaskID)
	err_ := os.MkdirAll(b.OutDir, 0755)
	if err_ != nil {
		return errs.New(core.ErrIOWriteFail, err_)
	}
	config.Args.Logfile = b.OutDir + "/out.log"
	log.Setup(config.Args.Debug, config.Args.Logfile)
	// 交易对初始化
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
	b.logState(btime.TimeMS())
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
	err = biz.OdMgr.CleanUp()
	if err != nil {
		log.Error("backtest clean orders fail", zap.Error(err))
		return
	}
	log.Info(fmt.Sprintf("Complete! cost: %.1fs, avg: %.1f bar/s", btCost, float64(b.BarNum)/btCost))
	b.printBtResult()
}

func (b *BackTest) onLiquidation(symbol string) {
	date := btime.ToDateStr(btime.TimeMS(), "")
	if config.ChargeOnBomb {
		oldVal := biz.Wallets.TotalLegal(nil, false)
		biz.InitFakeWallets(symbol)
		newVal := biz.Wallets.TotalLegal(nil, false)
		b.TotalInvest += newVal - oldVal
		log.Error(fmt.Sprintf("wallet %s BOMB at %s, reset wallet and continue..", symbol, date))
	} else {
		log.Error(fmt.Sprintf("wallet %s BOMB at %s, exit", symbol, date))
		core.StopAll()
	}
}

func (b *BackTest) orderCB(order *orm.InOutOrder, isEnter bool) {
	if isEnter {
		b.MaxOpenOrders = max(b.MaxOpenOrders, len(orm.OpenODs))
	} else if config.DrawBalanceOver > 0 {
		quoteLegal := biz.Wallets.AvaLegal(config.StakeCurrency)
		if quoteLegal > config.DrawBalanceOver {
			biz.Wallets.WithdrawLegal(quoteLegal-config.DrawBalanceOver, config.StakeCurrency)
		}
	}
}

func (b *BackTest) logState(timeMS int64) {
	if b.StartMS == 0 {
		b.StartMS = timeMS
	}
	b.EndMS = timeMS
	b.logPlot(timeMS)
	quoteLegal := biz.Wallets.TotalLegal(config.StakeCurrency, false)
	b.MinBalance = min(b.MinBalance, quoteLegal)
	b.MaxBalance = max(b.MaxBalance, quoteLegal)
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
			Real:          make([]float64, 0, newNum),
			Available:     make([]float64, 0, newNum),
			UnrealizedPOL: make([]float64, 0, newNum),
			WithDraw:      make([]float64, 0, newNum),
		}
		for i := 0; i < oldNum; i += splStep {
			plots.Labels = append(plots.Labels, b.Plots.Labels[i])
			plots.Real = append(plots.Real, b.Plots.Real[i])
			plots.Available = append(plots.Available, b.Plots.Available[i])
			plots.UnrealizedPOL = append(plots.UnrealizedPOL, b.Plots.UnrealizedPOL[i])
			plots.WithDraw = append(plots.WithDraw, b.Plots.WithDraw[i])
		}
		b.Plots = plots
		return
	}
	avaLegal := biz.Wallets.AvaLegal(nil)
	totalLegal := biz.Wallets.TotalLegal(nil, true)
	profitLegal := biz.Wallets.UnrealizedPOLLegal(nil)
	drawLegal := biz.Wallets.GetWithdrawLegal(nil)
	curDate := btime.ToDateStr(timeMS, "")
	b.Plots.Labels = append(b.Plots.Labels, curDate)
	b.Plots.Real = append(b.Plots.Real, totalLegal)
	b.Plots.Available = append(b.Plots.Available, avaLegal)
	b.Plots.UnrealizedPOL = append(b.Plots.UnrealizedPOL, profitLegal)
	b.Plots.WithDraw = append(b.Plots.WithDraw, drawLegal)
}
