package data

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"math"
	"slices"
	"strings"
)

type FnPairKline = func(bar *orm.InfoKline)
type FuncEnvEnd = func(bar *banexg.PairTFKline, adj *orm.AdjInfo)

type PairTFCache struct {
	TimeFrame  string
	TFSecs     int
	NextMS     int64         // 记录下一个期待收到的bar起始时间戳，如果不一致，则出现了bar缺失，需查询更新。
	WaitBar    *banexg.Kline // 记录尚未完成的bar。已完成时应置为nil
	Latest     *banexg.Kline // 记录最新bar数据，可能未完成，可能已完成
	AlignOffMS int64
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
	*orm.ExSymbol
	States   []*PairTFCache
	WaitBar  *banexg.Kline
	CallBack FnPairKline
	OnEnvEnd FuncEnvEnd                 // 期货主力切换或股票除权，需先平仓
	tfBars   map[string][]*banexg.Kline // 缓存各周期的原始K线（未复权）
	adjs     []*orm.AdjInfo             // 复权因子列表
	adj      *orm.AdjInfo
}

func (f *Feeder) getStates() []*PairTFCache {
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
func (f *Feeder) SubTfs(timeFrames []string, delOther bool) []string {
	var oldTfs = make(map[string]bool)
	var stateMap = make(map[string]*PairTFCache)
	var minTfSecs = 0 // 记录最小时间周期
	if len(f.States) > 0 {
		for _, sta := range f.States {
			oldTfs[sta.TimeFrame] = true
			stateMap[sta.TimeFrame] = sta
			if minTfSecs == 0 || sta.TFSecs < minTfSecs {
				minTfSecs = sta.TFSecs
			}
		}
	}
	// 新增的记录到adds中，已有的从oldTfs中删除，stateMap保留全部的
	exchange, err := exg.GetWith(f.Exchange, f.Market, "")
	if err != nil {
		log.Warn("get exchange fail", zap.String("ex", f.Exchange), zap.Error(err))
		return nil
	}
	exgID := exchange.Info().ID
	adds := make([]string, 0, len(timeFrames))
	for _, tf := range timeFrames {
		if _, ok := oldTfs[tf]; ok {
			delete(oldTfs, tf)
			continue
		}
		tfSecs := utils.TFToSecs(tf)
		sta := &PairTFCache{
			TimeFrame:  tf,
			TFSecs:     tfSecs,
			AlignOffMS: int64(exg.GetAlignOff(exgID, tfSecs) * 1000),
		}
		stateMap[tf] = sta
		if minTfSecs == 0 || sta.TFSecs < minTfSecs {
			minTfSecs = sta.TFSecs
		}
		adds = append(adds, tf)
	}
	// 如果需要删除未传入的，记录下最小周期的state，防止再次从空白重建
	var minDel *PairTFCache
	if delOther && len(oldTfs) > 0 {
		// 删除此次为传入的时间周期
		for tf := range oldTfs {
			if sta, ok := stateMap[tf]; ok {
				if sta.TFSecs == minTfSecs {
					minDel = sta
				}
				delete(stateMap, tf)
			}
		}
	}
	var newStates = utils.ValsOfMap(stateMap)
	// 对所有周期从小到大排序，第一个必须是后续所有states的最小公倍数，以便能从第一个更新后续所有
	slices.SortFunc(newStates, func(a, b *PairTFCache) int {
		return a.TFSecs - b.TFSecs
	})
	secs := make([]int, len(newStates))
	for i, v := range newStates {
		secs[i] = v.TFSecs
	}
	minSecs := utils.GcdInts(secs)
	if minSecs != newStates[0].TFSecs {
		if minDel != nil && minDel.TFSecs == minSecs {
			newStates = append([]*PairTFCache{minDel}, newStates...)
		} else {
			minTf := utils.SecsToTF(minSecs)
			newStates = append([]*PairTFCache{{TFSecs: minSecs, TimeFrame: minTf}}, newStates...)
		}
	}
	f.States = newStates
	return adds
}

/*
更新State并触发回调（内部自动复权）
bars 原始未复权的K线
*/
func (f *Feeder) onStateOhlcvs(state *PairTFCache, bars []*banexg.Kline, lastOk bool) []*banexg.Kline {
	if len(bars) == 0 {
		return nil
	}
	finishBars := bars
	if !lastOk {
		finishBars = bars[:len(bars)-1]
	}
	if state.WaitBar != nil && state.WaitBar.Time < bars[0].Time {
		finishBars = append([]*banexg.Kline{state.WaitBar}, finishBars...)
	}
	state.Latest = bars[len(bars)-1]
	state.WaitBar = nil
	if !lastOk {
		state.WaitBar = state.Latest
	}
	tfMSecs := int64(state.TFSecs * 1000)
	if len(finishBars) > 0 {
		state.NextMS = finishBars[len(finishBars)-1].Time + tfMSecs
		f.addTfKlines(state.TimeFrame, finishBars)
		adjBars := f.adj.Apply(finishBars, core.AdjFront)
		f.fireCallBacks(f.Symbol, state.TimeFrame, tfMSecs, adjBars, f.adj)
	}
	return finishBars
}

func (f *Feeder) getTfKlines(tf string, endMS int64, limit int, pBar *utils.PrgBar) ([]*banexg.Kline, *errs.Error) {
	bars, _ := f.tfBars[tf]
	if len(bars) > 0 {
		// 缓存有，直接返回
		bars = orm.ApplyAdj(f.adjs, bars, core.AdjFront, endMS, limit)
		if pBar != nil {
			pBar.Add(core.StepTotal)
		}
		return bars, nil
	}
	exchange, err := exg.GetWith(f.Exchange, f.Market, "")
	if err != nil {
		return nil, err
	}
	adjs, bars, err := orm.AutoFetchOHLCV(exchange, f.ExSymbol, tf, 0, endMS, limit, false, pBar)
	if err != nil {
		return nil, err
	}
	f.tfBars[tf] = bars
	bars = orm.ApplyAdj(adjs, bars, core.AdjFront, 0, 0)
	return bars, nil
}

func (f *Feeder) addTfKlines(tf string, bars []*banexg.Kline) {
	olds, _ := f.tfBars[tf]
	if len(olds) > core.NumTaCache*2 {
		olds = olds[len(olds)-core.NumTaCache*3/2:]
	}
	f.tfBars[tf] = append(olds, bars...)
}

func (f *Feeder) fireCallBacks(pair, timeFrame string, tfMSecs int64, bars []*banexg.Kline, adj *orm.AdjInfo) {
	isLive := core.LiveMode
	for _, bar := range bars {
		if !isLive {
			btime.CurTimeMS = bar.Time + tfMSecs
		}
		f.CallBack(&orm.InfoKline{
			PairTFKline: &banexg.PairTFKline{Kline: *bar, Symbol: pair, TimeFrame: timeFrame},
			Adj:         adj,
		})
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
		lastTime := bars[len(bars)-1].Time
		delay := btime.TimeMS() - (lastTime + tfMSecs)
		if delay > tfMSecs && tfMSecs >= 60000 {
			barNum := delay / tfMSecs
			log.Warn(fmt.Sprintf("%s/%s bar too late, delay %v bars, %v", pair, timeFrame, barNum, lastTime))
		}
	}
}

type IKlineFeeder interface {
	getSymbol() string
	getWaitBar() *banexg.Kline
	setWaitBar(bar *banexg.Kline)
	/*
		SubTfs 为当前标的订阅指定时间周期的数据，可多个
	*/
	SubTfs(timeFrames []string, delOther bool) []string
	/*
		WarmTfs 预热时间周期给定K线数量到指定时间
	*/
	WarmTfs(curMS int64, tfNums map[string]int, pBar *utils.PrgBar) (int64, *errs.Error)
	onNewBars(barTfMSecs int64, bars []*banexg.Kline) (bool, *errs.Error)
	getStates() []*PairTFCache
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
	PreFire  float64        // 提前触发bar的比率
	adjIdx   int            // adjs的索引
	warmNums map[string]int // 各周期预热数量
}

func NewKlineFeeder(exs *orm.ExSymbol, callBack FnPairKline) (*KlineFeeder, *errs.Error) {
	sess, conn, err := orm.Conn(nil)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	adjs, err := sess.GetAdjs(exs.ID)
	if err != nil {
		return nil, err
	}
	return &KlineFeeder{
		Feeder: Feeder{
			ExSymbol: exs,
			CallBack: callBack,
			tfBars:   make(map[string][]*banexg.Kline),
			adjs:     adjs,
		},
		PreFire: config.PreFire,
	}, nil
}

func (f *KlineFeeder) WarmTfs(curMS int64, tfNums map[string]int, pBar *utils.PrgBar) (int64, *errs.Error) {
	if len(tfNums) == 0 {
		tfNums = f.warmNums
		if len(tfNums) == 0 {
			return 0, nil
		}
	} else {
		f.warmNums = tfNums
	}
	maxEndMs := int64(0)
	for tf, warmNum := range tfNums {
		tfMSecs := int64(utils.TFToSecs(tf) * 1000)
		if tfMSecs < int64(60000) || warmNum <= 0 {
			continue
		}
		endMS := utils.AlignTfMSecs(curMS, tfMSecs)
		bars, err := f.getTfKlines(tf, endMS, warmNum, pBar)
		if err != nil {
			return 0, err
		}
		if len(bars) == 0 {
			log.Info("skip warm as empty", zap.String("pair", f.Symbol), zap.String("tf", tf),
				zap.Int("want", warmNum), zap.Int64("end", endMS))
			continue
		}
		if warmNum != len(bars) {
			barEndMs := bars[len(bars)-1].Time + tfMSecs
			barStartMs := bars[0].Time
			lackNum := warmNum - len(bars)
			log.Info(fmt.Sprintf("warm %s/%s lack %v bars, expect: %v, range:%v-%v", f.Symbol,
				tf, lackNum, warmNum, barStartMs, barEndMs))
		}
		curEnd := f.warmTf(tf, bars)
		maxEndMs = max(maxEndMs, curEnd)
	}
	return maxEndMs, nil
}

/*
WarmTfs
预热周期数据。当动态添加周期到已有的HistDataFeeder时，应调用此方法预热数据。
如果TaEnv已存在会被重置。

	LiveFeeder在初始化时也应当调用此函数
	传入的bars是复权后的K线

返回结束的时间戳（即下一个bar开始时间戳）
*/
func (f *KlineFeeder) warmTf(tf string, bars []*banexg.Kline) int64 {
	if len(bars) == 0 {
		return 0
	}
	tfMSecs := int64(utils.TFToSecs(tf) * 1000)
	lastMS := bars[len(bars)-1].Time + tfMSecs
	envKey := strings.Join([]string{f.Symbol, tf}, "_")
	if env, ok := strategy.Envs[envKey]; ok {
		env.Reset()
	}
	if len(f.adjs) > 0 {
		// 按复权信息分批调用
		cache := make([]*banexg.Kline, 0, len(bars))
		var pAdj = f.adjs[0]
		var pi = 1
		forEnd := false
		for i, k := range bars {
			for k.Time >= pAdj.StopMS {
				if len(cache) > 0 {
					f.fireCallBacks(f.Symbol, tf, tfMSecs, cache, pAdj)
					cache = make([]*banexg.Kline, 0, len(bars))
				}
				if pi >= len(f.adjs) {
					f.fireCallBacks(f.Symbol, tf, tfMSecs, bars[i:], nil)
					forEnd = true
					pAdj = nil
					break
				}
				pAdj = f.adjs[pi]
				pi += 1
			}
			if forEnd {
				break
			}
			cache = append(cache, k)
		}
		if len(cache) > 0 {
			f.fireCallBacks(f.Symbol, tf, tfMSecs, cache, pAdj)
		}
	} else {
		f.fireCallBacks(f.Symbol, tf, tfMSecs, bars, nil)
	}
	for _, sta := range f.States {
		if sta.TimeFrame == tf {
			sta.NextMS = lastMS
			break
		}
	}
	return lastMS
}

/*
onNewBars
有新完成的子周期蜡烛数据，尝试更新
bars 是未复权的K线，内部会进行复权
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
		ohlcvs, lastOk = utils.BuildOHLCV(bars, staMSecs, f.PreFire, olds, barTfMSecs, state.AlignOffMS)
	} else if barTfMSecs == staMSecs {
		ohlcvs, lastOk = bars, true
	} else {
		barTf := utils.SecsToTF(int(barTfMSecs / 1000))
		msg := fmt.Sprintf("bar intv invalid, expect %v, cur: %v", state.TimeFrame, barTf)
		return false, errs.NewMsg(core.ErrInvalidBars, msg)
	}
	if len(ohlcvs) == 0 {
		return false, nil
	}
	minState, minOhlcvs := state, ohlcvs
	// 应该按周期从大到小触发
	if len(f.States) > 1 {
		// 对于第2个及后续的粗粒度。从第一个得到的OHLC更新
		// 即使第一个没有完成，也要更新更粗周期维度，否则会造成数据丢失
		if barTfMSecs < staMSecs {
			// 这里应该保留最后未完成的数据
			ohlcvs, _ = utils.BuildOHLCV(bars, staMSecs, f.PreFire, nil, barTfMSecs, state.AlignOffMS)
		} else {
			ohlcvs = bars
		}
		for i := len(f.States) - 1; i >= 1; i-- {
			state = f.States[i]
			var olds []*banexg.Kline
			if state.WaitBar != nil {
				olds = append(olds, state.WaitBar)
			}
			bigTfMSecs := int64(state.TFSecs * 1000)
			curOhlcvs, lastOk := utils.BuildOHLCV(ohlcvs, bigTfMSecs, f.PreFire, olds, staMSecs, state.AlignOffMS)
			f.onStateOhlcvs(state, curOhlcvs, lastOk)
		}
	}
	//子序列周期维度<=当前维度。当收到spider发送的数据时，这里可能是3个或更多ohlcvs
	doneBars := f.onStateOhlcvs(minState, minOhlcvs, lastOk)
	return len(doneBars) > 0, nil
}

type IHistKlineFeeder interface {
	IKlineFeeder
	getNextMS() int64
	/*
		DownIfNeed 下载整个范围的K线，需在SetSeek前调用
	*/
	DownIfNeed(sess *orm.Queries, exchange banexg.BanExchange, pBar *utils.PrgBar) *errs.Error
	/*
		SetSeek 设置读取位置，在循环读取前调用
	*/
	SetSeek(since int64)
	/*
		GetBar 获取当前K线，然后可调用CallNext移动指针到下一个
	*/
	GetBar() *banexg.Kline
	/*
		RunBar 运行Bar对应的回调函数
	*/
	RunBar(bar *banexg.Kline) *errs.Error
	/*
		CallNext 移动指针到下一个K线
	*/
	CallNext()
}

/*
HistKLineFeeder
历史数据反馈器。是文件反馈器和数据库反馈器的基类。

	回测模式：每次读取3K个bar，按nextMS大小依次回测触发。
*/
type HistKLineFeeder struct {
	KlineFeeder
	TimeRange  *config.TimeTuple
	rowIdx     int             // 缓存中下一个Bar的索引，-1表示已结束
	caches     []*banexg.Kline // 缓存的Bar，逐个fire，读取完重新加载
	nextMS     int64           // 下一个bar的13位毫秒时间戳，math.MaxInt32表示结束
	minGapMs   int64           // caches中最小的间隔毫秒数
	setNext    func()
	TradeTimes [][2]int64 // 可交易时间
}

func (f *HistKLineFeeder) getNextMS() int64 {
	return f.nextMS
}

/*
获取当前bar，用于invokeBar；之后应调用callNext设置光标到下一个bar
*/
func (f *HistKLineFeeder) GetBar() *banexg.Kline {
	if f.rowIdx < 0 || f.rowIdx >= len(f.caches) {
		return nil
	}
	bar := f.caches[f.rowIdx]
	return bar
}

func (f *HistKLineFeeder) RunBar(bar *banexg.Kline) *errs.Error {
	_, err := f.onNewBars(f.minGapMs, []*banexg.Kline{bar})
	return err
}

func (f *HistKLineFeeder) CallNext() {
	f.setNext()
}

/*
DBKlineFeeder
数据库读取K线的Feeder，用于回测
*/
type DBKlineFeeder struct {
	HistKLineFeeder
	offsetMS int64
}

func NewDBKlineFeeder(exs *orm.ExSymbol, callBack FnPairKline) (*DBKlineFeeder, *errs.Error) {
	exchange, err := exg.GetWith(exs.Exchange, exs.Market, "")
	if err != nil {
		return nil, err
	}
	market, err := exchange.GetMarket(exs.Symbol)
	if err != nil {
		return nil, err
	}
	feeder, err := NewKlineFeeder(exs, callBack)
	if err != nil {
		return nil, err
	}
	res := &DBKlineFeeder{
		HistKLineFeeder: HistKLineFeeder{
			KlineFeeder: *feeder,
			TimeRange:   config.TimeRange,
			TradeTimes:  market.GetTradeTimes(),
		},
	}
	res.setNext = makeSetNext(res)
	return res, nil
}

func (f *DBKlineFeeder) SetSeek(since int64) {
	if since == 0 {
		// 这里不能为0，不然会从后往前读取K线，导致缺失
		since = core.MSMinStamp
	}
	f.offsetMS = since
	f.setNext()
}

/*
DownIfNeed
下载指定区间的数据
pBar 用于进度更新，总和为1000，每次更新此次的量
*/
func (f *DBKlineFeeder) DownIfNeed(sess *orm.Queries, exchange banexg.BanExchange, pBar *utils.PrgBar) *errs.Error {
	if len(f.States) == 0 {
		return nil
	}
	downTf, err := orm.GetDownTF(f.States[0].TimeFrame)
	if err != nil {
		if pBar != nil {
			pBar.Add(core.StepTotal)
		}
		return err
	}
	if sess == nil {
		ctx := context.Background()
		var conn *pgxpool.Conn
		sess, conn, err = orm.Conn(ctx)
		if err != nil {
			if pBar != nil {
				pBar.Add(core.StepTotal)
			}
			return err
		}
		defer conn.Release()
	}
	_, err = sess.DownOHLCV2DB(exchange, f.ExSymbol, downTf, f.TimeRange.StartMS, f.TimeRange.EndMS, pBar)
	return err
}

func (f *DBKlineFeeder) setAdjIdx() {
	for f.adjIdx < len(f.adjs) {
		adj := f.adjs[f.adjIdx]
		if f.nextMS < adj.StopMS {
			f.adj = adj
			return
		}
		f.adjIdx += 1
	}
	f.adj = nil
}

func makeSetNext(f *DBKlineFeeder) func() {
	return func() {
		if f.rowIdx+1 < len(f.caches) {
			f.rowIdx += 1
			f.nextMS = f.caches[f.rowIdx].Time
			if f.adj != nil && f.nextMS >= f.adj.StopMS {
				old := f.caches[f.rowIdx-1]
				tf := f.States[0].TimeFrame
				f.OnEnvEnd(&banexg.PairTFKline{
					Symbol:    f.Symbol,
					TimeFrame: tf,
					Kline:     *old,
				}, f.adj)
				// 重新复权预热
				core.IsWarmUp = true
				_, err := f.WarmTfs(f.nextMS, nil, nil)
				core.IsWarmUp = false
				if err != nil {
					log.Error("next warm tf fail", zap.Error(err))
				}
				f.setAdjIdx()
			}
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
		if f.nextMS+tfMSecs >= f.TimeRange.EndMS {
			f.rowIdx = -1
			f.nextMS = math.MaxInt64
			return
		}
		batchSize := 3000
		_, bars, err := sess.GetOHLCV(f.ExSymbol, state.TimeFrame, f.offsetMS, f.TimeRange.EndMS, batchSize, false)
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
		f.setAdjIdx()
		f.minGapMs = math.MaxInt64
		for i, b := range bars[1:] {
			gap := b.Time - bars[i].Time
			if gap < f.minGapMs {
				f.minGapMs = gap
			}
		}
	}
}
