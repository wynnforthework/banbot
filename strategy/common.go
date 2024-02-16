package strategy

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	ta "github.com/banbox/banta"
	"github.com/pkujhd/goloader"
	"go.uber.org/zap"
	"os"
	"path"
	"strings"
	"unsafe"
)

var (
	stagyMap = make(map[string]*TradeStagy) // 已加载的策略缓存
)

func Get(stagyName string) *TradeStagy {
	obj, ok := stagyMap[stagyName]
	if ok {
		return obj
	}
	obj = load(stagyName)
	if obj != nil {
		stagyMap[stagyName] = obj
	}
	return obj
}

func load(stagyName string) *TradeStagy {
	filePath := path.Join(config.GetStagyDir(), stagyName+".o")
	_, err := os.Stat(filePath)
	nameVar := zap.String("name", stagyName)
	if err != nil {
		log.Error("strategy not found", zap.String("path", filePath), zap.Error(err))
		return nil
	}
	linker, err := goloader.ReadObj(filePath, stagyName)
	if err != nil {
		log.Error("strategy load fail, package is `main`?", nameVar, zap.Error(err))
		return nil
	}
	symPtr := make(map[string]uintptr)
	err = goloader.RegSymbol(symPtr)
	if err != nil {
		log.Error("strategy read symbol fail", nameVar, zap.Error(err))
		return nil
	}
	regLoaderTypes(symPtr)
	module, err := goloader.Load(linker, symPtr)
	if err != nil {
		log.Error("strategy load module fail", nameVar, zap.Error(err))
		return nil
	}
	keys := zap.String("keys", strings.Join(utils.KeysOfMap(module.Syms), ","))
	prefix := stagyName + "."
	// 加载Main
	mainPath := prefix + "Main"
	mainPtr := GetModuleItem(module, mainPath)
	if mainPtr == nil {
		log.Error("module item not found", zap.String("p", mainPath), keys)
		return nil
	}
	runFunc := *(*func() *TradeStagy)(mainPtr)
	stagy := runFunc()
	stagy.Name = stagyName
	// 这里不能卸载，卸载后结构体的嵌入函数无法调用
	// module.Unload()
	return stagy
}

func GetModuleItem(module *goloader.CodeModule, itemPath string) unsafe.Pointer {
	main, ok := module.Syms[itemPath]
	if !ok || main == 0 {
		return nil
	}
	mainPtr := (uintptr)(unsafe.Pointer(&main))
	return unsafe.Pointer(&mainPtr)
}

func regLoaderTypes(symPtr map[string]uintptr) {
	goloader.RegTypes(symPtr, &ta.BarEnv{}, &ta.Series{}, &ta.CrossLog{}, &ta.XState{}, ta.Cross, ta.Sum, ta.SMA,
		ta.EMA, ta.EMABy, ta.RMA, ta.RMABy, ta.TR, ta.ATR, ta.MACD, ta.MACDBy, ta.RSI, ta.Highest, ta.Lowest, ta.KDJ,
		ta.KDJBy, ta.StdDev, ta.StdDevBy, ta.BBANDS, ta.TD, &ta.AdxState{}, ta.ADX, ta.ROC, ta.HeikinAshi)
	stgy := &TradeStagy{}
	goloader.RegTypes(symPtr, stgy, stgy.OnPairInfos, stgy.OnStartUp, stgy.OnBar, stgy.OnInfoBar, stgy.OnTrades,
		stgy.OnCheckExit, stgy.GetDrawDownExitRate, stgy.PickTimeFrame, stgy.OnShutDown)
	job := &StagyJob{}
	goloader.RegTypes(symPtr, job, job.OpenOrder)
	goloader.RegTypes(symPtr, &PairSub{}, &EnterReq{}, &ExitReq{})
}

func (q *ExitReq) Clone() *ExitReq {
	res := &ExitReq{
		Tag:        q.Tag,
		StgyName:   q.StgyName,
		EnterTag:   q.EnterTag,
		Dirt:       q.Dirt,
		OrderType:  q.OrderType,
		Limit:      q.Limit,
		ExitRate:   q.ExitRate,
		Amount:     q.Amount,
		OrderID:    q.OrderID,
		UnOpenOnly: q.UnOpenOnly,
		Force:      q.Force,
	}
	return res
}

func (s *StagyJob) InitBar(curOrders []*orm.InOutOrder) {
	s.CheckMS = btime.TimeMS()
	if core.IsWarmUp {
		s.Orders = nil
	} else if s.EnterNum > 0 {
		s.Orders = nil
		for _, od := range curOrders {
			if od.Strategy == s.Stagy.Name {
				s.Orders = append(s.Orders, od)
			}
		}
		s.EnterNum = len(s.Orders)
	}
	s.Entrys = nil
	s.Exits = nil
}

func (s *StagyJob) CheckCustomExits() ([]*orm.InOutEdit, *errs.Error) {
	var res []*orm.InOutEdit
	var skipSL, skipTP = 0, 0
	for _, od := range s.Orders {
		if !od.CanClose() {
			continue
		}
		slPrice := od.GetInfoFloat64(orm.KeyStopLossPrice)
		tpPrice := od.GetInfoFloat64(orm.KeyTakeProfitPrice)
		req, err := s.customExit(od)
		if err != nil {
			return res, err
		}
		if req == nil {
			// 检查是否需要修改条件单
			newSLPrice := s.LongSLPrice
			newTPPrice := s.LongTPPrice
			if od.Short {
				newSLPrice = s.ShortSLPrice
				newTPPrice = s.ShortTPPrice
			}
			if newSLPrice == 0 {
				newSLPrice = od.GetInfoFloat64(orm.KeyStopLossPrice)
			}
			if newTPPrice == 0 {
				newTPPrice = od.GetInfoFloat64(orm.KeyTakeProfitPrice)
			}
			if newSLPrice != slPrice {
				if s.ExgStopLoss {
					res = append(res, &orm.InOutEdit{
						Order:  od,
						Action: "StopLoss",
					})
					od.SetInfo(orm.KeyStopLossPrice, newSLPrice)
				} else {
					skipSL += 1
					od.SetInfo(orm.KeyStopLossPrice, nil)
				}
			}
			if newTPPrice != tpPrice {
				if s.ExgTakeProfit {
					res = append(res, &orm.InOutEdit{
						Order:  od,
						Action: "TakeProfit",
					})
					od.SetInfo(orm.KeyTakeProfitPrice, newTPPrice)
				} else {
					skipTP += 1
					od.SetInfo(orm.KeyTakeProfitPrice, nil)
				}
			}
		}
	}
	if core.LiveMode() && skipSL+skipTP > 0 {
		log.Warn(fmt.Sprintf("%s/%s triggers on exchange is disabled, stoploss: %v, takeprofit: %v",
			s.Stagy.Name, s.Symbol.Symbol, skipSL, skipTP))
	}
	return res, nil
}
