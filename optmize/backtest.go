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
)

type BackTest struct {
	biz.Trader
}

func NewBackTest() *BackTest {
	p := &BackTest{}
	data.Main = data.NewHistProvider(p.FeedKLine)
	return p
}

func (b *BackTest) Init() *errs.Error {
	core.RunMode = core.RunModeBackTest
	err := orm.InitTask()
	if err != nil {
		return err
	}
	exchange, err := exg.Get()
	if err != nil {
		return err
	}
	// 交易对初始化
	err = orm.EnsureExgSymbols(exchange)
	if err != nil {
		return err
	}
	err = orm.InitListDates()
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
	err = data.Main.SubWarmPairs(warms)
	if err != nil {
		return err
	}
	biz.InitFakeWallets()
	return nil
}

func (b *BackTest) FeedKLine(bar *banexg.PairTFKline) {
	err := b.Trader.FeedKline(bar)
	if err != nil {
		if err.Code == core.ErrLiquidation {
			b.onLiquidation(bar.Symbol)
		} else {
			log.Error("FeedKline fail", zap.String("p", bar.Symbol), zap.Error(err))
		}
		return
	}

}

func (b *BackTest) Run() {
	err := b.Init()
	if err != nil {
		log.Error("backtest init fail", zap.Error(err))
		return
	}
	err = data.Main.LoopMain()
	if err != nil {
		log.Error("backtest loop fail", zap.Error(err))
		return
	}
	err = biz.OdMgr.CleanUp()
	if err != nil {
		log.Error("backtest clean orders fail", zap.Error(err))
		return
	}
}

func (b *BackTest) onLiquidation(symbol string) {
	date := btime.ToDateStr(btime.TimeMS(), "")
	if config.ChargeOnBomb {
		biz.InitFakeWallets(symbol)
		log.Error(fmt.Sprintf("wallet %s BOMB at %s, reset wallet and continue..", symbol, date))
	} else {
		log.Error(fmt.Sprintf("wallet %s BOMB at %s, exit", symbol, date))
		core.StopAll()
	}
}
