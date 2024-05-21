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
	"go.uber.org/zap"
	"math"
	"sort"
	"sync"
)

var (
	Main IProvider
)

type IProvider interface {
	LoopMain() *errs.Error
	SubWarmPairs(items map[string]map[string]int, delOther bool) *errs.Error
	UnSubPairs(pairs ...string) *errs.Error
	SetDirty()
}

type Provider[T IKlineFeeder] struct {
	holders   map[string]T
	newFeeder func(pair string, tfs []string) (T, *errs.Error)
	dirty     bool
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

func (p *Provider[IKlineFeeder]) SetDirty() {
	p.dirty = true
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
	var oldSince = make(map[string]int64)
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
			} else {
				oldSince[fmt.Sprintf("%s|%s", pair, curMinTf)] = hold.getStates()[0].NextMS
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
	for key, since := range oldSince {
		sinceMap[key] = since
	}
	return newHolds, sinceMap, delPairs, err
}

func (p *Provider[IKlineFeeder]) warmJobs(warmJobs []*WarmJob) (map[string]int64, *errs.Error) {
	sinceMap := make(map[string]int64)
	lockMap := sync.Mutex{}
	jobNum := 0
	// 预热所需的必要数据
	for _, job := range warmJobs {
		jobNum += len(job.tfWarms)
	}
	log.Info(fmt.Sprintf("warmup for %d pairs, %v jobs", len(warmJobs), jobNum))
	pBar := utils.NewPrgBar(jobNum*core.StepTotal, "warmup")
	defer pBar.Close()
	retErr := utils.ParallelRun(warmJobs, core.ConcurNum, func(job *WarmJob) *errs.Error {
		hold := job.hold
		since, err := hold.warmTfs(btime.TimeMS(), job.tfWarms, pBar)
		lockMap.Lock()
		sinceMap[hold.getSymbol()] = since
		lockMap.Unlock()
		return err
	})
	return sinceMap, retErr
}

type HistProvider[T IHistKlineFeeder] struct {
	Provider[T]
}

func InitHistProvider(callBack FnPairKline, envEnd FuncEnvEnd) {
	Main = &HistProvider[IHistKlineFeeder]{
		Provider: Provider[IHistKlineFeeder]{
			holders: make(map[string]IHistKlineFeeder),
			newFeeder: func(pair string, tfs []string) (IHistKlineFeeder, *errs.Error) {
				feeder, err := NewDBKlineFeeder(pair, callBack)
				if err != nil {
					return nil, err
				}
				feeder.onEnvEnd = envEnd
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
	pBar := utils.NewPrgBar(len(p.holders)*core.StepTotal, "DownHist")
	defer pBar.Close()
	for _, h := range p.holders {
		err = h.downIfNeed(sess, exchange, pBar)
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
	maxSince := int64(0)
	holders := make(map[string]IHistKlineFeeder)
	for pair, since := range sinceMap {
		hold, ok := p.holders[pair]
		if !ok {
			continue
		}
		holders[pair] = hold
		if hold.getNextMS() == 0 || hold.getStates()[0].NextMS != since {
			// 这里忽略刷新交易对后，仍然存在的标的
			hold.initNext(since)
		}
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
	var pBar = utils.NewPrgBar(int(totalMS), "RunHist")
	defer pBar.Close()
	log.Info("run data loop for backtest..")
	var hold IHistKlineFeeder
	holds := utils.ValsOfMap(p.holders)
	p.dirty = true
	for {
		if !core.BotRunning {
			break
		}
		if p.dirty {
			holds = utils.ValsOfMap(p.holders)
			holds = p.sortFeeders(holds, hold, false)
			p.dirty = false
		} else {
			holds = p.sortFeeders(holds, hold, true)
		}
		hold = holds[0]
		if hold.getNextMS() == math.MaxInt64 {
			break
		}
		holds = holds[1:]
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
			pBar.Add(int(pBarAdd))
			pbarTo = btime.TimeMS()
		}
	}
	return nil
}

func (p *HistProvider[IHistKlineFeeder]) sortFeeders(holds []IHistKlineFeeder, hold IHistKlineFeeder, insert bool) []IHistKlineFeeder {
	if insert {
		// 插入排序，说明holds已有序，二分查找位置，最快排序
		vb := hold.getNextMS()
		bSymbol := hold.getSymbol()
		index := sort.Search(len(holds), func(i int) bool {
			va := holds[i].getNextMS()
			if va == math.MaxInt64 || vb == math.MaxInt64 {
				return va > vb
			}
			if va != vb {
				return va > vb
			}
			return holds[i].getSymbol() > bSymbol
		})
		holds = append(holds, hold)
		copy(holds[index+1:], holds[index:])
		holds[index] = hold
		return holds
	}
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
	return holds
}

type LiveProvider[T IKlineFeeder] struct {
	Provider[T]
	*KLineWatcher
}

func InitLiveProvider(callBack FnPairKline, envEnd FuncEnvEnd) *errs.Error {
	watcher, err := NewKlineWatcher(config.SpiderAddr)
	if err != nil {
		return err
	}
	provider := &LiveProvider[IKlineFeeder]{
		Provider: Provider[IKlineFeeder]{
			holders: make(map[string]IKlineFeeder),
			newFeeder: func(pair string, tfs []string) (IKlineFeeder, *errs.Error) {
				feeder, err := NewKlineFeeder(pair, callBack)
				if err != nil {
					return nil, err
				}
				feeder.subTfs(tfs, false)
				feeder.onEnvEnd = envEnd
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
			if since, ok := sinceMap[symbol]; ok {
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
		// 已在启动或休市期间计算复权因子，内部会自动进行复权
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
