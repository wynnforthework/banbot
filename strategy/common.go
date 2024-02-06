package strategy

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
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
	file, err := os.Open(filePath)
	if err != nil {
		log.Error("strategy read fail", nameVar, zap.Error(err))
		return nil
	}
	linker, err := goloader.ReadObj(file, &stagyName)
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
	main, ok := module.Syms[fmt.Sprintf("%s.Main", stagyName)]
	if !ok || main == 0 {
		keys := zap.String("keys", strings.Join(utils.KeysOfMap(module.Syms), ","))
		log.Error("strategy `Main` not found", nameVar, zap.Error(err), keys)
		return nil
	}
	mainPtr := (uintptr)(unsafe.Pointer(&main))
	runFunc := *(*func() *TradeStagy)(unsafe.Pointer(&mainPtr))
	stagy := runFunc()
	stagy.Name = stagyName
	module.Unload()
	return stagy
}

func regLoaderTypes(symPtr map[string]uintptr) {
	goloader.RegTypes(symPtr, ta.ATR, ta.Highest, ta.Lowest)
	job := &StagyJob{}
	goloader.RegTypes(symPtr, job.OpenOrder)
}

func (q *ExitReq) Clone() *ExitReq {
	return &ExitReq{
		Tag:        q.Tag,
		Dirt:       q.Dirt,
		StgyName:   q.StgyName,
		OrderType:  q.OrderType,
		Limit:      q.Limit,
		ExitRate:   q.ExitRate,
		Amount:     q.Amount,
		EnterTag:   q.EnterTag,
		OrderID:    q.OrderID,
		UnOpenOnly: q.UnOpenOnly,
		Force:      q.Force,
	}
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

func (s *StagyJob) CheckCustomExits() []*orm.InOutEdit {
	var res []*orm.InOutEdit
	var skipSL, skipTP = 0, 0
	for _, od := range s.Orders {
		if !od.CanClose() {
			continue
		}
		slPrice := od.GetInfoFloat64(orm.KeyStopLossPrice)
		tpPrice := od.GetInfoFloat64(orm.KeyTakeProfitPrice)
		req := s.Stagy.OnCheckExit(s, od)
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
	return res
}
