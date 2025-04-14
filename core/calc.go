package core

import (
	"github.com/sasha-s/go-deadlock"
	"math"
)

type Ema struct {
	Alpha float64
	Val   float64
	Age   int
}

func NewEMA(alpha float64) *Ema {
	return &Ema{Alpha: alpha}
}

func (e *Ema) Update(val float64) float64 {
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return math.NaN()
	}
	if e.Val == 0 {
		e.Val = val
	} else {
		e.Val = e.Val*(1-e.Alpha) + val*e.Alpha
	}
	e.Age += 1
	return e.Val
}

func (e *Ema) Reset() {
	e.Val = 0
	e.Age = 0
}

/*
NumSet 时间间隔内数据收集
*/
type NumSet struct {
	Stamp     int64
	AlignUnit int64
	Data      map[string]float64
	CallBack  func(int64, map[string]float64)
	Lock      deadlock.Mutex
}

func NewNumSet(alignUnit int64, callback func(int64, map[string]float64)) *NumSet {
	return &NumSet{
		AlignUnit: alignUnit,
		Data:      make(map[string]float64),
		CallBack:  callback,
	}
}

func (ns *NumSet) Update(stamp int64, key string, val float64) {
	ns.Lock.Lock()
	if ns.AlignUnit > 0 {
		stamp = stamp / ns.AlignUnit * ns.AlignUnit
	}
	if stamp > ns.Stamp {
		if len(ns.Data) > 0 {
			ns.CallBack(ns.Stamp, ns.Data)
		}
		ns.Stamp = stamp
		ns.Data = make(map[string]float64)
	}
	ns.Data[key] = val
	ns.Lock.Unlock()
}
