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
	StagyMap = make(map[string]*TradeStagy) // 已加载的策略缓存
)

func Get(stagyName string) *TradeStagy {
	obj, ok := StagyMap[stagyName]
	if ok {
		if obj.Name == "" {
			initStrategy(stagyName, obj)
		}
		return obj
	}
	obj = loadNative(stagyName)
	initStrategy(stagyName, obj)
	if obj != nil {
		StagyMap[stagyName] = obj
	}
	return obj
}

func initStrategy(name string, stgy *TradeStagy) {
	stgy.Name = name
	if stgy.MinTfScore == 0 {
		stgy.MinTfScore = 0.8
	}
}

func loadNative(stagyName string) *TradeStagy {
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

func (s *StagyJob) SnapOrderStates() map[int64]*orm.InOutSnap {
	var res = make(map[int64]*orm.InOutSnap)
	for _, od := range s.Orders {
		res[od.ID] = od.TakeSnap()
	}
	return res
}

func (s *StagyJob) CheckCustomExits(snap map[int64]*orm.InOutSnap) ([]*orm.InOutEdit, *errs.Error) {
	var res []*orm.InOutEdit
	var skipSL, skipTP = 0, 0
	for _, od := range s.Orders {
		shot := od.TakeSnap()
		old, ok := snap[od.ID]
		if !od.CanClose() || od.Status < orm.InOutStatusFullEnter {
			// 尚未完全入场，检查限价或触发价格是否更新
			if !ok {
				continue
			}
			if old.EnterLimit != shot.EnterLimit {
				// 修改入场价格
				res = append(res, &orm.InOutEdit{
					Order:  od,
					Action: "LimitEnter",
				})
			}
		} else {
			// 已完全入场的，检查是否退出
			slEdit, tpEdit, err := s.checkOrderExit(od)
			if err != nil {
				return res, err
			}
			if slEdit != nil {
				if slEdit.Action == "" {
					skipSL += 1
				} else {
					res = append(res, slEdit)
				}
			} else if old != nil && (old.StopLoss != shot.StopLoss || old.StopLossLimit != shot.StopLossLimit) {
				// 在其他地方更新了止损
				res = append(res, &orm.InOutEdit{Order: od, Action: "StopLoss"})
			}
			if tpEdit != nil {
				if tpEdit.Action == "" {
					skipSL += 1
				} else {
					res = append(res, tpEdit)
				}
			} else if old != nil && (old.TakeProfit != shot.TakeProfit || old.TakeProfitLimit != shot.TakeProfitLimit) {
				// 在其他地方更新了止盈
				res = append(res, &orm.InOutEdit{Order: od, Action: "TakeProfit"})
			}
			if old.ExitLimit != shot.ExitLimit {
				// 修改出场价格
				res = append(res, &orm.InOutEdit{
					Order:  od,
					Action: "LimitExit",
				})
			}
		}
	}
	if core.LiveMode && skipSL+skipTP > 0 {
		log.Warn(fmt.Sprintf("%s/%s triggers on exchange is disabled, stoploss: %v, takeprofit: %v",
			s.Stagy.Name, s.Symbol.Symbol, skipSL, skipTP))
	}
	return res, nil
}

func (s *StagyJob) checkOrderExit(od *orm.InOutOrder) (*orm.InOutEdit, *orm.InOutEdit, *errs.Error) {
	slPrice := od.GetInfoFloat64(orm.OdInfoStopLoss)
	tpPrice := od.GetInfoFloat64(orm.OdInfoTakeProfit)
	slLimit := od.GetInfoFloat64(orm.OdInfoStopLossLimit)
	tpLimit := od.GetInfoFloat64(orm.OdInfoTakeProfitLimit)
	req, err := s.customExit(od)
	if err != nil {
		return nil, nil, err
	}
	var slEdit, tpEdit *orm.InOutEdit
	if req == nil {
		// 检查是否需要修改条件单
		newSLPrice := s.LongSLPrice
		newTPPrice := s.LongTPPrice
		if od.Short {
			newSLPrice = s.ShortSLPrice
			newTPPrice = s.ShortTPPrice
		}
		if newSLPrice == 0 {
			newSLPrice = od.GetInfoFloat64(orm.OdInfoStopLoss)
		}
		if newTPPrice == 0 {
			newTPPrice = od.GetInfoFloat64(orm.OdInfoTakeProfit)
		}
		newSLLimit := od.GetInfoFloat64(orm.OdInfoStopLossLimit)
		newTPLimit := od.GetInfoFloat64(orm.OdInfoTakeProfitLimit)
		if newSLPrice != slPrice || slLimit != newSLLimit {
			if s.ExgStopLoss {
				slEdit = &orm.InOutEdit{Order: od, Action: "StopLoss"}
				od.SetInfo(orm.OdInfoStopLoss, newSLPrice)
				od.SetInfo(orm.OdInfoStopLossLimit, newSLLimit)
			} else {
				slEdit = &orm.InOutEdit{}
				od.SetInfo(orm.OdInfoStopLoss, nil)
				od.SetInfo(orm.OdInfoStopLossLimit, nil)
			}
		}
		if newTPPrice != tpPrice || tpLimit != newTPLimit {
			if s.ExgTakeProfit {
				tpEdit = &orm.InOutEdit{Order: od, Action: "TakeProfit"}
				od.SetInfo(orm.OdInfoTakeProfit, newTPPrice)
				od.SetInfo(orm.OdInfoTakeProfitLimit, newTPLimit)
			} else {
				tpEdit = &orm.InOutEdit{}
				od.SetInfo(orm.OdInfoTakeProfit, nil)
				od.SetInfo(orm.OdInfoTakeProfitLimit, nil)
			}
		}
	}
	return slEdit, tpEdit, nil
}

func GetJobs(account string) map[string]map[string]*StagyJob {
	if !core.EnvReal {
		account = config.DefAcc
	}
	jobs, ok := AccJobs[account]
	if !ok {
		jobs = map[string]map[string]*StagyJob{}
		AccJobs[account] = jobs
	}
	return jobs
}

func GetInfoJobs(account string) map[string]map[string]*StagyJob {
	if !core.EnvReal {
		account = config.DefAcc
	}
	jobs, ok := AccInfoJobs[account]
	if !ok {
		jobs = map[string]map[string]*StagyJob{}
		AccInfoJobs[account] = jobs
	}
	return jobs
}
