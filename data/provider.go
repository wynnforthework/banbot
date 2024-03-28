package data

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/schollz/progressbar/v3"
	"go.uber.org/zap"
	"math"
	"sort"
	"strings"
	"sync"
)

var (
	Main IProvider
)

type IProvider interface {
	LoopMain() *errs.Error
	SubWarmPairs(items map[string]map[string]int, delOther bool) *errs.Error
	UnSubPairs(pairs ...string) *errs.Error
}

type Provider[T IKlineFeeder] struct {
	holders   map[string]T
	newFeeder func(pair string, tfs []string) (T, *errs.Error)
}

func (p *Provider[IKlineFeeder]) UnSubPairs(pairs ...string) []string {
	var removed []string
	for _, pair := range pairs {
		if _, ok := p.holders[pair]; ok {
			delete(p.holders, pair)
			removed = append(removed, pair)
		}
	}
	return removed
}

type WarmJob struct {
	hold    IKlineFeeder
	tfWarms map[string]int
}

/*
SubWarmPairs
从数据提供者添加新的交易对订阅。

	items: pair[timeFrame]warmNum
	返回最小周期变化的交易对(新增/旧对新周期)、预热任务
*/
func (p *Provider[IKlineFeeder]) SubWarmPairs(items map[string]map[string]int, delOther bool) ([]IKlineFeeder, map[string]int64, []string, *errs.Error) {
	core.IsWarmUp = true
	defer func() {
		core.IsWarmUp = false
	}()
	var newHolds []IKlineFeeder
	var warmJobs []*WarmJob
	var err *errs.Error
	for pair, tfWarms := range items {
		hold, ok := p.holders[pair]
		if !ok {
			hold, err = p.newFeeder(pair, utils.KeysOfMap(tfWarms))
			if err != nil {
				return nil, nil, nil, err
			}
			p.holders[pair] = hold
			newHolds = append(newHolds, hold)
			warmJobs = append(warmJobs, &WarmJob{hold: hold, tfWarms: tfWarms})
		} else {
			oldMinTf := hold.getStates()[0].TimeFrame
			newTfs := hold.subTfs(utils.KeysOfMap(tfWarms), delOther)
			curMinTf := hold.getStates()[0].TimeFrame
			if oldMinTf != curMinTf {
				newHolds = append(newHolds, hold)
			}
			if len(newTfs) > 0 {
				warmJobs = append(warmJobs, &WarmJob{
					hold:    hold,
					tfWarms: utils.CutMap(tfWarms, newTfs...),
				})
			}
		}
	}
	var delPairs []string
	if delOther {
		for pair := range p.holders {
			if _, ok := items[pair]; !ok {
				delete(p.holders, pair)
				delPairs = append(delPairs, pair)
			}
		}
	}
	// 加载数据预热
	sinceMap, err := p.warmJobs(warmJobs)
	return newHolds, sinceMap, delPairs, err
}

func (p *Provider[IKlineFeeder]) warmJobs(warmJobs []*WarmJob) (map[string]int64, *errs.Error) {
	sinceMap := make(map[string]int64)
	lockMap := sync.Mutex{}
	exchange := exg.Default
	curTimeMS := btime.TimeMS()
	jobNum := 0
	var retErr *errs.Error
	// 预热所需的必要数据
	for _, job := range warmJobs {
		jobNum += len(job.tfWarms)
	}
	log.Info(fmt.Sprintf("warmup for %d pairs, %v jobs", len(warmJobs), jobNum))
	doneNum := 0
	barTotalNum := int64(jobNum * core.StepTotal)
	pBar := progressbar.Default(barTotalNum, "warmup")
	defer pBar.Close()
	var m sync.Mutex
	stepCB := func(num int) {
		m.Lock()
		defer m.Unlock()
		doneNum += num
		if int64(doneNum) > barTotalNum {
			log.Warn("warm pBar progress exceed", zap.Int64("max", barTotalNum), zap.Int("cur", doneNum))
			return
		}
		err := pBar.Add(num)
		if err != nil {
			log.Error("add pBar fail for warmup", zap.Error(err))
		}
	}
	// 控制同时预热下载的标的数量
	guard := make(chan struct{}, core.ConcurNum)
	var wg sync.WaitGroup
	for _, job_ := range warmJobs {
		// 如果达到并发限制，这里会阻塞等待
		guard <- struct{}{}
		if retErr != nil {
			// 下载出错，终端返回
			break
		}
		wg.Add(1)
		go func(job *WarmJob) {
			defer func() {
				// 完成一个任务，从chan弹出一个
				<-guard
				wg.Done()
			}()
			symbol := job.hold.getSymbol()
			exs, err := orm.GetExSymbol(exchange, symbol)
			if err != nil {
				retErr = err
				stepCB(core.StepTotal * len(job.tfWarms))
				return
			}
			for tf, warmNum := range job.tfWarms {
				key := fmt.Sprintf("%s|%s", symbol, tf)
				lockMap.Lock()
				sinceMap[key] = 0
				lockMap.Unlock()
				tfMSecs := int64(utils.TFToSecs(tf) * 1000)
				if tfMSecs < int64(60000) {
					stepCB(core.StepTotal)
					continue
				}
				endMS := utils.AlignTfMSecs(curTimeMS, tfMSecs)
				bars, err := orm.AutoFetchOHLCV(exchange, exs, tf, 0, endMS, warmNum, false, stepCB)
				if err != nil {
					retErr = err
					break
				}
				if len(bars) == 0 {
					log.Warn("skip warm as empty", zap.String("pair", exs.Symbol), zap.String("tf", tf),
						zap.Int("want", warmNum), zap.Int64("end", endMS))
					continue
				}
				if warmNum != len(bars) {
					barEndMs := bars[len(bars)-1].Time + tfMSecs
					barStartMs := bars[0].Time
					lackNum := warmNum - len(bars)
					log.Warn(fmt.Sprintf("warm %s/%s lack %v bars, expect: %v, range:%v-%v", exs.Symbol,
						tf, lackNum, warmNum, barStartMs, barEndMs))
				}
				sinceVal := job.hold.WarmTfs(map[string][]*banexg.Kline{tf: bars})
				lockMap.Lock()
				sinceMap[key] = sinceVal
				lockMap.Unlock()
			}
		}(job_)
	}
	wg.Wait()
	if barTotalNum-int64(doneNum) > 0 {
		stepCB(int(barTotalNum) - doneNum)
	}
	return sinceMap, retErr
}

type HistProvider[T IHistKlineFeeder] struct {
	Provider[T]
}

func InitHistProvider(callBack FnPairKline) {
	Main = &HistProvider[IHistKlineFeeder]{
		Provider: Provider[IHistKlineFeeder]{
			holders: make(map[string]IHistKlineFeeder),
			newFeeder: func(pair string, tfs []string) (IHistKlineFeeder, *errs.Error) {
				feeder, err := NewDBKlineFeeder(pair, callBack)
				if err != nil {
					return nil, err
				}
				feeder.subTfs(tfs, false)
				return feeder, nil
			},
		},
	}
}

func (p *HistProvider[IHistKlineFeeder]) downIfNeed() *errs.Error {
	var err *errs.Error
	exchange := exg.Default
	sess, conn, err := orm.Conn(nil)
	if err != nil {
		return err
	}
	defer conn.Release()
	var pBar = progressbar.Default(int64(len(p.holders)*core.StepTotal), "DownHist")
	defer pBar.Close()
	var m sync.Mutex
	stepCB := func(num int) {
		m.Lock()
		defer m.Unlock()
		err_ := pBar.Add(num)
		if err_ != nil {
			log.Error("update pBar fail", zap.Error(err_))
		}
	}
	for _, h := range p.holders {
		err = h.downIfNeed(sess, exchange, stepCB)
		if err != nil {
			log.Error("download ohlcv fail", zap.String("pair", h.getSymbol()), zap.Error(err))
			return err
		}
	}
	return nil
}

func (p *HistProvider[IHistKlineFeeder]) SubWarmPairs(items map[string]map[string]int, delOther bool) *errs.Error {
	_, sinceMap, _, err := p.Provider.SubWarmPairs(items, delOther)
	// 检查回测期间数据是否需要下载，如需要自动下载
	err = p.downIfNeed()
	if err != nil {
		return err
	}
	pairSince := make(map[string]int64)
	for key, val := range sinceMap {
		pair := strings.Split(key, "|")[0]
		if oldVal, ok := pairSince[pair]; ok && oldVal > 0 {
			// 大周期的sinceMS可能小于小周期的，这里应该取最大时间。
			pairSince[pair] = max(oldVal, val)
		} else {
			pairSince[pair] = val
		}
	}
	maxSince := int64(0)
	holders := make(map[string]IHistKlineFeeder)
	for pair, since := range pairSince {
		hold, ok := p.holders[pair]
		if !ok {
			continue
		}
		holders[pair] = hold
		hold.initNext(since)
		maxSince = max(maxSince, since)
	}
	// 删除未预热的项
	p.holders = holders
	btime.CurTimeMS = maxSince
	return err
}

func (p *HistProvider[IHistKlineFeeder]) UnSubPairs(pairs ...string) *errs.Error {
	_ = p.Provider.UnSubPairs(pairs...)
	return nil
}

func (p *HistProvider[IHistKlineFeeder]) LoopMain() *errs.Error {
	if len(p.holders) == 0 {
		return errs.NewMsg(core.ErrBadConfig, "no pairs to run")
	}
	totalMS := (config.TimeRange.EndMS - config.TimeRange.StartMS) / 1000
	var pbarTo = config.TimeRange.StartMS
	var pBar = progressbar.Default(totalMS, "RunHist")
	defer func() {
		err_ := pBar.Close()
		if err_ != nil {
			log.Error("procBar close fail", zap.Error(err_))
		}
	}()
	log.Info("run data loop for backtest..")
	for {
		if !core.BotRunning {
			break
		}
		holds := utils.ValsOfMap(p.holders)
		sort.Slice(holds, func(i, j int) bool {
			a, b := holds[i], holds[j]
			va, vb := a.getNextMS(), b.getNextMS()
			if va == math.MaxInt64 || vb == math.MaxInt64 {
				return va < vb
			}
			if va != vb {
				return va < vb
			}
			return a.getSymbol() < b.getSymbol()
		})
		hold := holds[0]
		if hold.getNextMS() == math.MaxInt64 {
			break
		}
		// 触发回调
		err := hold.invoke()
		if err != nil {
			if err.Code == core.ErrEOF {
				break
			}
			log.Error("data loop main fail", zap.Error(err))
			return err
		}
		// 更新进度条
		pBarAdd := (btime.TimeMS() - pbarTo) / 1000
		if pBarAdd > 0 {
			err_ := pBar.Add64(pBarAdd)
			pbarTo = btime.TimeMS()
			if err_ != nil {
				log.Error("procBar add fail", zap.Error(err_))
				return errs.New(core.ErrRunTime, err_)
			}
		}
	}
	return nil
}

type LiveProvider[T IKlineFeeder] struct {
	Provider[T]
	*KLineWatcher
}

func InitLiveProvider(callBack FnPairKline) *errs.Error {
	watcher, err := NewKlineWatcher(config.SpiderAddr)
	if err != nil {
		return err
	}
	provider := &LiveProvider[IKlineFeeder]{
		Provider: Provider[IKlineFeeder]{
			holders: make(map[string]IKlineFeeder),
			newFeeder: func(pair string, tfs []string) (IKlineFeeder, *errs.Error) {
				feeder := NewKlineFeeder(pair, callBack)
				feeder.subTfs(tfs, false)
				return feeder, nil
			},
		},
		KLineWatcher: watcher,
	}
	watcher.OnKLineMsg = makeOnKlineMsg(provider)
	// 立刻订阅实时价格
	err = watcher.SendMsg("subscribe", []string{
		fmt.Sprintf("price_%s_%s", core.ExgName, core.Market),
	})
	if err != nil {
		return err
	}
	Main = provider
	return nil
}

func (p *LiveProvider[IKlineFeeder]) SubWarmPairs(items map[string]map[string]int, delOther bool) *errs.Error {
	newHolds, sinceMap, delPairs, err := p.Provider.SubWarmPairs(items, delOther)
	if err != nil {
		return err
	}
	if len(newHolds) > 0 {
		var jobs []WatchJob
		for _, h := range newHolds {
			symbol, timeFrame := h.getSymbol(), h.getStates()[0].TimeFrame
			key := fmt.Sprintf("%s|%s", symbol, timeFrame)
			if since, ok := sinceMap[key]; ok {
				jobs = append(jobs, WatchJob{
					Symbol:    symbol,
					TimeFrame: timeFrame,
					Since:     since,
				})
			}
		}
		err = p.WatchJobs(core.ExgName, core.Market, "ohlcv", jobs...)
		if err != nil {
			return err
		}
		if len(core.BookPairs) > 0 {
			jobs = make([]WatchJob, 0, len(core.BookPairs))
			for pair := range core.BookPairs {
				jobs = append(jobs, WatchJob{Symbol: pair, TimeFrame: "1m"})
			}
			err = p.WatchJobs(core.ExgName, core.Market, "book", jobs...)
			if err != nil {
				return err
			}
		}
	}
	if len(delPairs) > 0 {
		err = p.UnWatchJobs(core.ExgName, core.Market, "ohlcv", delPairs)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *LiveProvider[IKlineFeeder]) UnSubPairs(pairs ...string) *errs.Error {
	removed := p.Provider.UnSubPairs(pairs...)
	if len(removed) == 0 {
		return nil
	}
	return p.UnWatchJobs(core.ExgName, core.Market, "ohlcv", pairs)
}

func (p *LiveProvider[IKlineFeeder]) LoopMain() *errs.Error {
	return p.RunForever()
}

func makeOnKlineMsg(p *LiveProvider[IKlineFeeder]) func(msg *KLineMsg) {
	return func(msg *KLineMsg) {
		if msg.ExgName != core.ExgName || msg.Market != core.Market {
			return
		}
		hold, ok := p.holders[msg.Pair]
		if !ok {
			return
		}
		tfMSecs := int64(msg.TFSecs * 1000)
		if msg.Interval >= msg.TFSecs {
			_, err := hold.onNewBars(tfMSecs, msg.Arr)
			if err != nil {
				log.Error("onNewBars fail", zap.String("p", msg.Pair), zap.Error(err))
			}
			return
		}
		// 更新频率低于bar周期，收到的可能未完成
		lastIdx := len(msg.Arr) - 1
		doneArr, lastBar := msg.Arr[:lastIdx], msg.Arr[lastIdx]
		waitBar := hold.getWaitBar()
		if waitBar != nil && waitBar.Time < lastBar.Time {
			doneArr = append([]*banexg.Kline{waitBar}, doneArr...)
			hold.setWaitBar(nil)
		}
		if len(doneArr) > 0 {
			_, err := hold.onNewBars(tfMSecs, doneArr)
			if err != nil {
				log.Error("onNewBars fail", zap.String("p", msg.Pair), zap.Error(err))
			}
			return
		}
		if msg.Interval <= 5 && hold.getStates()[0].TFSecs >= 60 {
			// 更新很快，需要的周期相对较长，则要求出现下一个bar时认为完成（走上面逻辑）
			hold.setWaitBar(lastBar)
			return
		}
		// 更新频率相对不高，或占需要的周期比率较大，近似完成认为完成
		endLackSecs := int((lastBar.Time + tfMSecs - btime.TimeMS()) / 1000)
		if endLackSecs*2 < msg.Interval {
			// 缺少的时间不足更新间隔的一半，认为完成。
			_, err := hold.onNewBars(tfMSecs, []*banexg.Kline{lastBar})
			if err != nil {
				log.Error("onNewBars fail", zap.String("p", msg.Pair), zap.Error(err))
			}
		} else {
			hold.setWaitBar(lastBar)
		}
	}
}
