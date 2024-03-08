package data

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"math"
	"slices"
)

type FnPairKline = func(bar *banexg.PairTFKline)

type PairTFCache struct {
	TimeFrame string
	TFSecs    int
	NextMS    int64         // 下一个需要的13位时间戳。一般和WaitBar不应该同时使用
	WaitBar   *banexg.Kline // 记录尚未完成的bar。已完成时应置为nil
	Latest    *banexg.Kline // 记录最新bar数据，可能未完成，可能已完成
}

/*
Feeder
每个Feeder对应一个交易对。可包含多个时间维度。

	支持动态添加时间维度。
	回测模式：根据Feeder的下次更新时间，按顺序调用执行回调。
	实盘模式：订阅此交易对时间周期的新数据，被唤起时执行回调。
	支持预热数据。每个策略+交易对全程单独预热，不可交叉预热，避免btime被污染。
	LiveFeeder新交易对和新周期都需要预热；HistFeeder仅新周期需要预热
*/
type Feeder struct {
	Symbol   string
	States   []PairTFCache
	WaitBar  *banexg.Kline
	CallBack FnPairKline
}

func (f *Feeder) getStates() []PairTFCache {
	return f.States
}

func (f *Feeder) getSymbol() string {
	return f.Symbol
}

func (f *Feeder) getWaitBar() *banexg.Kline {
	return f.WaitBar
}

func (f *Feeder) setWaitBar(bar *banexg.Kline) {
	f.WaitBar = bar
}

/*
subTfs
添加监听到States中，返回新增的TimeFrames
*/
func (f *Feeder) subTfs(timeFrames ...string) []string {
	var oldTfs = make(map[string]bool)
	if len(f.States) > 0 {
		for _, sta := range f.States {
			oldTfs[sta.TimeFrame] = true
		}
	}
	adds := make([]string, 0, len(timeFrames))
	for _, tf := range timeFrames {
		if _, ok := oldTfs[tf]; ok {
			continue
		}
		f.States = append(f.States, PairTFCache{
			TimeFrame: tf,
			TFSecs:    utils.TFToSecs(tf),
		})
		adds = append(adds, tf)
	}
	slices.SortFunc(f.States, func(a, b PairTFCache) int {
		return a.TFSecs - b.TFSecs
	})
	secs := make([]int, len(f.States))
	for i, v := range f.States {
		secs[i] = v.TFSecs
	}
	minSecs := utils.GcdInts(secs)
	if minSecs != f.States[0].TFSecs {
		minTf := utils.SecsToTF(minSecs)
		f.States = append([]PairTFCache{{TFSecs: minSecs, TimeFrame: minTf}}, f.States...)
	}
	return adds
}

/*
更新State并触发回调
*/
func (f *Feeder) onStateOhlcvs(state PairTFCache, bars []*banexg.Kline, lastOk, doFire bool) []*banexg.Kline {
	if len(bars) == 0 {
		return nil
	}
	finishBars := bars
	if !lastOk {
		finishBars = bars[:len(bars)-1]
	}
	if state.WaitBar != nil && state.WaitBar.Time < finishBars[0].Time {
		finishBars = append([]*banexg.Kline{state.WaitBar}, finishBars...)
	}
	last := bars[len(bars)-1]
	state.WaitBar = nil
	if !lastOk {
		state.WaitBar = last
	}
	tfMSecs := int64(state.TFSecs * 1000)
	state.Latest = last
	state.NextMS = last.Time + tfMSecs
	if len(finishBars) > 0 && doFire {
		f.fireCallBacks(f.Symbol, state.TimeFrame, tfMSecs, finishBars)
	}
	return finishBars
}

func (f *Feeder) fireCallBacks(pair, timeFrame string, tfMSecs int64, bars []*banexg.Kline) {
	isLive := core.LiveMode
	for _, bar := range bars {
		if !isLive {
			btime.CurTimeMS = bar.Time + tfMSecs
		}
		f.CallBack(&banexg.PairTFKline{Kline: *bar, Symbol: pair, TimeFrame: timeFrame})
	}
	if isLive && !core.IsWarmUp {
		// 记录收到的bar数量
		hits, ok := core.TfPairHits[timeFrame]
		if !ok {
			hits = make(map[string]int)
			core.TfPairHits[timeFrame] = hits
		}
		num, _ := hits[pair]
		hits[pair] = num + len(bars)
		// 检查是否延迟
		delay := btime.TimeMS() - (bars[len(bars)-1].Time + tfMSecs)
		if delay > tfMSecs && tfMSecs >= 60000 {
			minutes := delay / tfMSecs
			log.Warn(fmt.Sprintf("%s/%s bar too late, delay %v mins", pair, timeFrame, minutes))
		}
	}
}

type IKlineFeeder interface {
	getSymbol() string
	getWaitBar() *banexg.Kline
	setWaitBar(bar *banexg.Kline)
	subTfs(timeFrames ...string) []string
	WarmTfs(tfBars map[string][]*banexg.Kline) int64
	onNewBars(barTfMSecs int64, bars []*banexg.Kline) (bool, *errs.Error)
	getStates() []PairTFCache
}

/*
KlineFeeder
每个Feeder对应一个交易对。可包含多个时间维度。实盘使用。

	支持动态添加时间维度。
	支持返回预热数据。每个策略+交易对全程单独预热，不可交叉预热，避免btime被污染。

	回测模式：使用派生结构体：DbKlineFeeder

	实盘模式：订阅此交易对时间周期的新数据，被唤起时执行回调。
	检查此交易对是否已在spider监听刷新，如没有则发消息给爬虫监听。
*/
type KlineFeeder struct {
	Feeder
	NextExpectMS int64
	PreFire      float64
}

func NewKlineFeeder(symbol string, callBack FnPairKline) *KlineFeeder {
	return &KlineFeeder{
		Feeder: Feeder{
			Symbol:   symbol,
			CallBack: callBack,
		},
		PreFire: config.PreFire,
	}
}

/*
WarmTfs
预热周期数据。当动态添加周期到已有的HistDataFeeder时，应调用此方法预热数据。

	LiveFeeder在初始化时也应当调用此函数

返回结束的时间戳（即下一个bar开始时间戳）
*/
func (f *KlineFeeder) WarmTfs(tfBars map[string][]*banexg.Kline) int64 {
	core.IsWarmUp = true
	maxEndMS := int64(0)
	for tf, bars := range tfBars {
		if len(bars) == 0 {
			continue
		}
		tfMSecs := int64(utils.TFToSecs(tf) * 1000)
		lastMS := bars[len(bars)-1].Time + tfMSecs
		f.fireCallBacks(f.Symbol, tf, tfMSecs, bars)
		for _, sta := range f.States {
			if sta.TimeFrame == tf {
				sta.NextMS = lastMS
				break
			}
		}
		maxEndMS = max(maxEndMS, lastMS)
	}
	core.IsWarmUp = false
	return maxEndMS
}

/*
onNewBars
有新完成的子周期蜡烛数据，尝试更新
*/
func (f *KlineFeeder) onNewBars(barTfMSecs int64, bars []*banexg.Kline) (bool, *errs.Error) {
	state := f.States[0]
	staMSecs := int64(state.TFSecs * 1000)
	var ohlcvs []*banexg.Kline
	var lastOk bool
	if barTfMSecs < staMSecs {
		var olds []*banexg.Kline
		if state.WaitBar != nil {
			olds = append(olds, state.WaitBar)
		}
		ohlcvs, lastOk = utils.BuildOHLCV(bars, state.TFSecs, f.PreFire, olds, barTfMSecs)
	} else if barTfMSecs == staMSecs {
		ohlcvs, lastOk = bars, true
	} else {
		msg := fmt.Sprintf("bar intv invalid, expect %v, cur: %v s", state.TimeFrame, barTfMSecs/1000)
		return false, errs.NewMsg(core.ErrInvalidBars, msg)
	}
	if len(ohlcvs) == 0 {
		return false, nil
	}
	//子序列周期维度<=当前维度。当收到spider发送的数据时，这里可能是3个或更多ohlcvs
	doneBars := f.onStateOhlcvs(state, ohlcvs, lastOk, true)
	if len(f.States) > 1 {
		// 对于第2个及后续的粗粒度。从第一个得到的OHLC更新
		// 即使第一个没有完成，也要更新更粗周期维度，否则会造成数据丢失
		if barTfMSecs < staMSecs {
			// 这里应该保留最后未完成的数据
			ohlcvs, _ = utils.BuildOHLCV(bars, state.TFSecs, f.PreFire, nil, barTfMSecs)
		} else {
			ohlcvs = bars
		}
		for _, state = range f.States[1:] {
			var olds []*banexg.Kline
			if state.WaitBar != nil {
				olds = append(olds, state.WaitBar)
			}
			curOhlcvs, lastOk := utils.BuildOHLCV(ohlcvs, state.TFSecs, f.PreFire, olds, staMSecs)
			f.onStateOhlcvs(state, curOhlcvs, lastOk, true)
		}
	}
	return len(doneBars) > 0, nil
}

type IHistKlineFeeder interface {
	IKlineFeeder
	getNextMS() int64
	initNext(since int64)
	getTotalLen() int
	invoke() (int, *errs.Error)
	downIfNeed(sess *orm.Queries, exchange banexg.BanExchange, stepCB func(num int)) *errs.Error
}

/*
HistKLineFeeder
历史数据反馈器。是文件反馈器和数据库反馈器的基类。

	回测模式：每次读取3K个bar，按nextMS大小依次回测触发。
*/
type HistKLineFeeder struct {
	KlineFeeder
	TimeRange *config.TimeTuple
	TotalLen  int
	rowIdx    int             // 缓存中下一个Bar的索引，-1表示已结束
	caches    []*banexg.Kline // 缓存的Bar，逐个fire，读取完重新加载
	nextMS    int64           // 下一个bar的13位毫秒时间戳，math.MaxInt32表示结束
	setNext   func()
}

func (f *HistKLineFeeder) getNextMS() int64 {
	return f.nextMS
}

func (f *HistKLineFeeder) initNext(since int64) {
	f.setNext()
}

func (f *HistKLineFeeder) getTotalLen() int {
	return f.TotalLen
}

func (f *HistKLineFeeder) invoke() (int, *errs.Error) {
	if f.rowIdx >= len(f.caches) {
		return 0, errs.NewMsg(core.ErrEOF, fmt.Sprintf("%s no more bars", f.Symbol))
	}
	bar := f.caches[f.rowIdx]
	tfMSecs := f.caches[1].Time - f.caches[0].Time
	_, err := f.onNewBars(tfMSecs, []*banexg.Kline{bar})
	f.setNext()
	count := 1
	if f.rowIdx >= 0 {
		count = int((f.nextMS - bar.Time) / tfMSecs)
	}
	return count, err
}

/*
DBKlineFeeder
数据库读取K线的Feeder，用于回测
*/
type DBKlineFeeder struct {
	HistKLineFeeder
	offsetMS int64
	exs      *orm.ExSymbol
}

func NewDBKlineFeeder(symbol string, callBack FnPairKline) (*DBKlineFeeder, *errs.Error) {
	exs, err := orm.GetExSymbolCur(symbol)
	if err != nil {
		return nil, err
	}
	res := &DBKlineFeeder{
		HistKLineFeeder: HistKLineFeeder{
			KlineFeeder: *NewKlineFeeder(symbol, callBack),
			TimeRange:   config.TimeRange,
			TotalLen:    -1,
		},
		exs: exs,
	}
	res.setNext = makeSetNext(res)
	return res, nil
}

func (f *DBKlineFeeder) initNext(since int64) {
	f.offsetMS = since
	f.setNext()
}

/*
downIfNeed
下载指定区间的数据
stepCB 用于进度更新，总和为1000，每次更新此次的量
*/
func (f *DBKlineFeeder) downIfNeed(sess *orm.Queries, exchange banexg.BanExchange, stepCB func(num int)) *errs.Error {
	downTf, err := orm.GetDownTF(f.States[0].TimeFrame)
	if err != nil {
		if stepCB != nil {
			stepCB(core.StepTotal)
		}
		return err
	}
	_, err = sess.DownOHLCV2DB(exchange, f.exs, downTf, f.TimeRange.StartMS, f.TimeRange.EndMS, stepCB)
	return err
}

func makeSetNext(f *DBKlineFeeder) func() {
	return func() {
		if f.rowIdx+1 < len(f.caches) {
			f.rowIdx += 1
			f.nextMS = f.caches[f.rowIdx].Time
			return
		}
		// 缓存读取完毕，重新读取数据库
		state := f.States[0]
		tfMSecs := int64(state.TFSecs * 1000)
		sess, conn, err := orm.Conn(nil)
		if err != nil {
			f.rowIdx = -1
			f.nextMS = math.MaxInt64
			log.Error("get conn fail while loading kline", zap.Error(err))
			return
		}
		defer conn.Release()
		if f.TotalLen < 0 {
			f.TotalLen = 1
			startMS, stopMS := sess.GetKlineRange(f.exs.ID, state.TimeFrame)
			if startMS > 0 && stopMS > 0 {
				startMS = max(startMS, f.TimeRange.StartMS)
				stopMS = min(stopMS, f.TimeRange.EndMS)
			} else {
				startMS = f.TimeRange.StartMS
				stopMS = f.TimeRange.EndMS
			}
			if startMS < stopMS {
				f.TotalLen = int((stopMS-startMS)/1000)/state.TFSecs + 1
			}
		}
		if f.nextMS+tfMSecs >= f.TimeRange.EndMS {
			f.rowIdx = -1
			f.nextMS = math.MaxInt64
			return
		}
		batchSize := 3000
		bars, err := sess.QueryOHLCV(f.exs.ID, state.TimeFrame, f.offsetMS, f.TimeRange.EndMS, batchSize, false)
		if err != nil || len(bars) == 0 {
			f.rowIdx = -1
			f.nextMS = math.MaxInt64
			if err != nil {
				log.Error("load kline fail", zap.Error(err))
			}
			return
		}
		f.caches = bars
		f.rowIdx = 0
		f.nextMS = bars[0].Time
		f.offsetMS = bars[len(bars)-1].Time + tfMSecs
	}
}
