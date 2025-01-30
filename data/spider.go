package data

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	"go.uber.org/zap"
	"strings"
	"sync"
	"time"
)

var (
	Spider *LiveSpider
)

type NotifyKLines struct {
	TFSecs   int
	Interval int // 推送更新间隔, <= TFSecs
	Arr      []*banexg.Kline
}

type KLineMsg struct {
	NotifyKLines
	ExgName string // The name of the exchange 交易所名称
	Market  string // market 市场
	Pair    string // symbol  币种
}

/*
getCheckInterval
Based on the trading pair and timeframe being monitored. Calculate the minimum check interval.

< 60s to fetch data through WebSocket, check the update interval can be relatively small.

	If the data is 1 m or more and obtained through the second-level interface of the API, it will be updated once every 3s

根据监听的交易对和时间帧。计算最小检查间隔。

	<60s的通过WebSocket获取数据，检查更新间隔可以比较小。
	1m及以上的通过API的秒级接口获取数据，3s更新一次
*/
func getCheckInterval(tfSecs int) float64 {
	var checkIntv float64

	switch {
	case tfSecs <= 3:
		checkIntv = 0.5
	case tfSecs <= 10:
		checkIntv = float64(tfSecs) * 0.35
	case tfSecs <= 60:
		checkIntv = float64(tfSecs) * 0.2
	case tfSecs <= 300:
		checkIntv = float64(tfSecs) * 0.15
	case tfSecs <= 900:
		checkIntv = float64(tfSecs) * 0.1
	case tfSecs <= 3600:
		checkIntv = float64(tfSecs) * 0.07
	default:
		// 超过1小时维度的，10分钟刷新一次
		checkIntv = 600
	}
	return checkIntv
}

/** *******************************  Spider 爬虫部分   ****************************
 */
var (
	initSids  = map[int32]bool{} // Mark whether the SID has been initialized or not 标记sid是否已初始化数据
	initMutex sync.Mutex         // Prevent initSids from concurrent reads and writes 防止出现initSids并发读和写
	writeQ    = make(chan *SaveKline, 1000)
)

type SaveKline struct {
	Sid       int32
	TimeFrame string
	Arr       []*banexg.Kline
	SkipFirst bool
	MsgAction string
}

func fillPrevHole(sess *orm.Queries, save *SaveKline) (int64, *errs.Error) {
	if save.SkipFirst {
		save.Arr = save.Arr[1:]
	}
	_, endMS := sess.GetKlineRange(save.Sid, save.TimeFrame)
	if len(save.Arr) == 0 {
		return endMS, nil
	}
	initMutex.Lock()
	initSids[save.Sid] = true
	initMutex.Unlock()
	fetchEndMS := save.Arr[0].Time

	var err *errs.Error
	if endMS == 0 || fetchEndMS <= endMS {
		// The new coin has no historical data, or the current bar and the inserted data are continuous, and the subsequent new bar can be directly inserted
		// 新的币无历史数据、或当前bar和已插入数据连续，直接插入后续新bar即可
		log.Info("first fetch ok", zap.Int32("sid", save.Sid), zap.Int64("end", endMS))
		return endMS, nil
	}
	exs := orm.GetSymbolByID(save.Sid)
	tfMSecs := int64(utils2.TFToSecs(save.TimeFrame) * 1000)
	tryCount := 0
	log.Debug("start first fetch", zap.String("pair", exs.Symbol), zap.Int64("s", endMS), zap.Int64("e", fetchEndMS))
	exchange, err := exg.GetWith(exs.Exchange, exs.Market, "")
	if err != nil {
		return endMS, err
	}
	var newEndMS = endMS
	var saveNum int
	for tryCount <= 5 {
		tryCount += 1
		saveNum, err = sess.DownOHLCV2DB(exchange, exs, save.TimeFrame, endMS, fetchEndMS, nil)
		if err != nil {
			_, endMS = sess.GetKlineRange(save.Sid, save.TimeFrame)
			return endMS, err
		}
		saveBars, err := sess.QueryOHLCV(exs.ID, save.TimeFrame, 0, 0, 1, false)
		if err != nil {
			_, endMS = sess.GetKlineRange(save.Sid, save.TimeFrame)
			return endMS, err
		}
		var lastMS = int64(0)
		if len(saveBars) > 0 {
			lastMS = saveBars[len(saveBars)-1].Time
			newEndMS = lastMS + tfMSecs
		}
		if newEndMS >= fetchEndMS {
			break
		} else {
			//If the latest bar is not obtained, wait for 2s to try again (the request ohlcv may not be obtained at the end of 1M)
			//如果未成功获取最新的bar，等待2s重试（1m刚结束时请求ohlcv可能取不到）
			log.Info("query first fail, wait 2s", zap.String("pair", exs.Symbol),
				zap.Int("ins", saveNum), zap.Int64("last", lastMS))
			time.Sleep(time.Second * 2)
		}
	}
	log.Info("first fetch ok", zap.String("pair", exs.Symbol), zap.Int64("s", endMS),
		zap.Int64("e", fetchEndMS))
	return newEndMS, nil
}

func consumeWriteQ(workNum int) {
	guard := make(chan struct{}, workNum)
	defer close(guard)
	setOne := func() {
		logged := false
		for {
			select {
			case guard <- struct{}{}:
				return
			case <-time.After(20 * time.Second):
				if !logged {
					log.Error("wait save in spider timeout")
					logged = true
				}
			}
		}
	}
	hourStamps := make(map[int32]int64)
	hourLock := sync.Mutex{}
	hourMSecs := int64(utils2.TFToSecs("1h") * 1000)
	for save := range writeQ {
		setOne()
		go func(job *SaveKline) {
			defer func() {
				<-guard
			}()
			ctx := context.Background()
			sess, conn, err := orm.Conn(ctx)
			if err == nil {
				defer conn.Release()
				var addBars = job.Arr
				initMutex.Lock()
				_, ok := initSids[job.Sid]
				initMutex.Unlock()
				if !ok {
					var nextMS int64
					nextMS, err = fillPrevHole(sess, job)
					var cutIdx = 0
					for i, bar := range job.Arr {
						if bar.Time < nextMS {
							cutIdx = i + 1
						} else {
							break
						}
					}
					addBars = job.Arr[cutIdx:]
				}
				if err == nil && len(addBars) > 0 {
					_, err = sess.InsertKLinesAuto(job.TimeFrame, job.Sid, addBars, true)
					// 下载1h及以上周期K线数据
					hourLock.Lock()
					lastMS, _ := hourStamps[job.Sid]
					hourAlign := utils2.AlignTfMSecs(btime.TimeMS(), hourMSecs)
					if hourAlign > lastMS {
						hourStamps[job.Sid] = hourAlign
					}
					hourLock.Unlock()
					if lastMS == 0 {
						kinfos, _ := sess.FindKInfos(ctx, job.Sid)
						for _, kinfo := range kinfos {
							if kinfo.Timeframe == "1h" {
								lastMS = kinfo.Stop
								break
							}
						}
					}
					if hourAlign > lastMS {
						exs := orm.GetSymbolByID(job.Sid)
						if exs == nil {
							log.Error("sid not found in cache", zap.Int32("sid", job.Sid))
						} else {
							var exchange banexg.BanExchange
							exchange, err = exg.GetWith(exs.Exchange, exs.Market, "")
							if err == nil {
								_, err = sess.DownOHLCV2DB(exchange, exs, "1h", lastMS, hourAlign, nil)
							}
						}
					}
				}
			}
			if err != nil {
				log.Error("consumeWriteQ: fail", zap.Int32("sid", job.Sid), zap.Error(err))
			}
			// After the K-line is written to the database, a message will be sent to notify the robot to avoid repeated insertion of K-line
			// 写入K线到数据库后，才发消息通知机器人，避免重复插入K线
			tfSecs := utils2.TFToSecs(job.TimeFrame)
			err = Spider.Broadcast(&utils.IOMsg{
				Action: job.MsgAction,
				Data: NotifyKLines{
					TFSecs:   tfSecs,
					Interval: tfSecs,
					Arr:      job.Arr,
				},
			})
			if err != nil {
				log.Error("broadCast kline fail", zap.String("action", job.MsgAction), zap.Error(err))
			}
		}(save)
	}
}

type FetchJob struct {
	PairTFCache
	Pair      string
	CheckSecs int
	Since     int64
	NextRun   int64
}

type Miner struct {
	spider       *LiveSpider
	ExgName      string
	Market       string
	exchange     banexg.BanExchange
	Fetchs       map[string]*FetchJob
	KlineReady   bool
	KlinePairs   map[string]bool
	TradeReady   bool
	TradePairs   map[string]bool
	BookReady    bool
	BookPairs    map[string]bool
	IsWatchPrice bool
	klineStates  map[string]*SubKLineState
}

type LiveSpider struct {
	*utils.ServerIO
	miners map[string]*Miner
}

func newMiner(spider *LiveSpider, exgName, market string) (*Miner, *errs.Error) {
	exchange, err := exg.GetWith(exgName, market, "")
	if err != nil {
		return nil, err
	}
	return &Miner{
		spider:      spider,
		ExgName:     exgName,
		Market:      market,
		exchange:    exchange,
		Fetchs:      map[string]*FetchJob{},
		KlinePairs:  map[string]bool{},
		TradePairs:  map[string]bool{},
		BookPairs:   map[string]bool{},
		klineStates: map[string]*SubKLineState{},
	}, nil
}

func (m *Miner) init() {
	_, err := orm.LoadMarkets(m.exchange, false)
	if err != nil {
		log.Error("load markets for miner fail", zap.String("exg", m.ExgName), zap.Error(err))
	}
}

func (m *Miner) SubPairs(jobType string, pairs ...string) *errs.Error {
	valids, _ := m.exchange.CheckSymbols(pairs...)
	if len(valids) == 0 {
		if len(pairs) > 0 {
			return nil
		}
		// If the incoming is empty, take all the underlying of the current exchange + market
		// 传入为空，取当前交易所+市场的所有标的
		markets := m.exchange.GetCurMarkets()
		valids = make([]string, 0, len(markets))
		for _, mar := range markets {
			valids = append(valids, mar.Symbol)
		}
	}
	ensures := make([]*orm.ExSymbol, 0, len(valids))
	for _, p := range pairs {
		ensures = append(ensures, &orm.ExSymbol{
			Exchange: m.ExgName,
			Market:   m.Market,
			Symbol:   p,
		})
	}
	err := orm.EnsureSymbols(ensures, m.ExgName)
	if err != nil {
		return err
	}
	if jobType == "ws" || jobType == "book" {
		m.watchOdBooks(valids)
	} else if jobType == "ohlcv" || jobType == "uohlcv" {
		m.watchKLines(valids)
	} else if jobType == "price" {
		m.watchPrices()
	} else if jobType == "trade" {
		m.watchTrades(valids)
	} else {
		log.Error("unknown sub type", zap.String("val", jobType))
	}
	return nil
}

func (m *Miner) UnSubPairs(jobType string, pairs ...string) *errs.Error {
	if jobType == "ws" || jobType == "book" {
		return m.exchange.UnWatchOrderBooks(pairs, nil)
	} else if jobType == "ohlcv" || jobType == "uohlcv" {
		jobs := make([][2]string, len(m.KlinePairs))
		timeFrame := "1s"
		if banexg.IsContract(m.Market) {
			timeFrame = "1m"
		}
		for p := range m.KlinePairs {
			jobs = append(jobs, [2]string{p, timeFrame})
		}
		return m.exchange.UnWatchOHLCVs(jobs, nil)
	} else if jobType == "price" {
		return m.exchange.UnWatchMarkPrices(nil, nil)
	} else if jobType == "trade" {
		return m.exchange.UnWatchTrades(pairs, nil)
	} else {
		log.Error("unknown unsub type", zap.String("val", jobType))
	}
	return nil
}

func (m *Miner) watchTrades(pairs []string) {
	if len(pairs) == 0 {
		return
	}
	for _, p := range pairs {
		m.TradePairs[p] = true
	}
	allPairs := utils.KeysOfMap(m.TradePairs)
	out, err := m.exchange.WatchTrades(allPairs, nil)
	if err != nil {
		log.Error("watch trades fail", zap.String("exg", m.ExgName), zap.Error(err))
		return
	}
	if m.TradeReady {
		return
	}
	m.TradeReady = true
	log.Info("start watch trades", zap.String("exg", m.ExgName), zap.Int("num", len(m.TradePairs)))
	prefix := fmt.Sprintf("trade_%s_%s_", m.ExgName, m.Market)

	go func() {
		defer func() {
			m.TradeReady = false
			log.Info("watch trades stopped, retry after 3s", zap.String("exg", m.ExgName))
			time.Sleep(time.Millisecond * 3200)
			m.watchTrades(utils.KeysOfMap(m.TradePairs))
		}()
		for item := range out {
			err = m.spider.Broadcast(&utils.IOMsg{
				Action: prefix,
				Data:   item,
			})
			if err != nil {
				log.Error("broadCast trade fail", zap.String("key", prefix), zap.Error(err))
			}
		}
	}()
}

func (m *Miner) watchPrices() {
	if m.IsWatchPrice {
		return
	}
	out, err := m.exchange.WatchMarkPrices(nil, map[string]interface{}{
		banexg.ParamInterval: "1s",
	})
	if err != nil {
		log.Error("watch prices fail", zap.String("exg", m.ExgName), zap.Error(err))
		return
	}
	m.IsWatchPrice = true
	log.Info("start watch prices", zap.String("exg", m.ExgName))
	prefix := fmt.Sprintf("price_%s_%s", m.ExgName, m.Market)

	go func() {
		defer func() {
			m.IsWatchPrice = false
			log.Info("watch prices stopped, retry after 3s", zap.String("exg", m.ExgName))
			time.Sleep(time.Millisecond * 2900)
			m.watchPrices()
		}()
		for item := range out {
			err = m.spider.Broadcast(&utils.IOMsg{
				Action: prefix,
				Data:   item,
			})
			if err != nil {
				log.Error("broadCast price fail", zap.String("key", prefix), zap.Error(err))
			}
		}
	}()
}

func (m *Miner) watchOdBooks(pairs []string) {
	if len(pairs) == 0 {
		return
	}
	for _, p := range pairs {
		m.BookPairs[p] = true
	}
	allPairs := utils.KeysOfMap(m.BookPairs)
	out, err := m.exchange.WatchOrderBooks(allPairs, 0, nil)
	if err != nil {
		log.Error("watch odBook fail", zap.String("exg", m.ExgName), zap.Error(err))
		return
	}
	if m.BookReady {
		return
	}
	m.BookReady = true
	log.Info("start watch odBooks", zap.String("exg", m.ExgName), zap.Int("num", len(m.BookPairs)))
	prefix := fmt.Sprintf("book_%s_%s_", m.ExgName, m.Market)

	go func() {
		defer func() {
			m.BookReady = false
			log.Info("watch odBook stopped, retry after 3s", zap.String("exg", m.ExgName))
			time.Sleep(time.Second * 3)
			m.watchOdBooks(utils.KeysOfMap(m.BookPairs))
		}()
		for book := range out {
			err = m.spider.Broadcast(&utils.IOMsg{
				Action: prefix + book.Symbol,
				Data:   book,
			})
			if err != nil {
				log.Error("broadCast odBook fail", zap.String("pair", book.Symbol), zap.Error(err))
			}
		}
	}()
}

type SubKLineState struct {
	Sid        int32
	NextNotify float64
	ExpectMS   int64
	PrevBar    *banexg.Kline
}

/*
这里将订阅此市场的最小周期(1s/1m)；1h/1d等大周期已在writeQ消费端判断并fetch
*/
func (m *Miner) watchKLines(pairs []string) {
	if len(pairs) == 0 {
		return
	}
	for _, p := range pairs {
		m.KlinePairs[p] = true
	}
	jobs := make([][2]string, 0, len(m.KlinePairs))
	timeFrame := "1s"
	if banexg.IsContract(m.Market) {
		timeFrame = "1m"
	}
	tfSecs := utils2.TFToSecs(timeFrame)
	tfMSecs := int64(tfSecs * 1000)
	expectMS := utils2.AlignTfMSecs(btime.TimeMS(), tfMSecs)
	for p := range m.KlinePairs {
		jobs = append(jobs, [2]string{p, timeFrame})
		if _, ok := m.klineStates[p]; !ok {
			exs, err := orm.GetExSymbol(m.exchange, p)
			if err != nil {
				code := fmt.Sprintf("%s.%s.%s", m.ExgName, m.Market, p)
				log.Error("invalid watchKLines", zap.String("pair", code), zap.Error(err))
				continue
			}
			res := &SubKLineState{
				Sid:      exs.ID,
				ExpectMS: expectMS,
			}
			m.klineStates[p] = res
		}
	}
	out, err := m.exchange.WatchOHLCVs(jobs, nil)
	if err != nil {
		log.Error("watch kline fail", zap.String("exg", m.ExgName),
			zap.Strings("pairs", pairs), zap.Error(err))
		return
	}
	if m.KlineReady {
		return
	}
	m.KlineReady = true
	log.Info("start watch kline", zap.String("exg", m.ExgName), zap.Int("num", len(m.KlinePairs)))
	prefix := fmt.Sprintf("ohlcv_%s_%s_", m.ExgName, m.Market)
	unPrefix := "u" + prefix

	// The candlestick is received, sent to the robot, and saved to the database
	// 收到K线，发送到机器人，保存到数据库
	handleSubKLines := func(key string, arr []*banexg.Kline) {
		parts := strings.Split(key, "_")
		pair, curTF := parts[0], parts[1]
		state, ok := m.klineStates[pair]
		if !ok {
			code := fmt.Sprintf("%s.%s.%s", m.ExgName, m.Market, pair)
			log.Warn("no pair state: " + code)
			return
		}
		curTS := btime.Time()
		var err_ *errs.Error
		// Send uohlcv subscription messages
		// 发送uohlcv订阅消息
		if curTS > state.NextNotify {
			err_ = m.spider.Broadcast(&utils.IOMsg{
				Action: unPrefix + pair,
				Data: NotifyKLines{
					TFSecs:   tfSecs,
					Interval: 1,
					Arr:      arr,
				},
			})
			// A maximum of 1 K-line message can be sent in 1s
			// 1s最多发送1次k线消息
			state.NextNotify = curTS + 0.9
		}
		// Check the completed candlesticks
		// 检查已完成的k线
		finishes := arr[:len(arr)-1]
		last := arr[len(arr)-1]
		if state.PrevBar != nil && last.Time > state.PrevBar.Time {
			if len(finishes) == 0 || state.PrevBar.Time < finishes[0].Time {
				finishes = append([]*banexg.Kline{state.PrevBar}, finishes...)
			}
		}
		if state.PrevBar == nil || last.Time >= state.PrevBar.Time {
			state.PrevBar = last
			state.ExpectMS = last.Time
		}
		if len(finishes) > 0 {
			// There are completed k-lines, written to the database, and only then the message is broadcast
			// 有已完成的k线，写入到数据库，然后才广播消息
			writeQ <- &SaveKline{
				Sid:       state.Sid,
				TimeFrame: curTF,
				Arr:       finishes,
				SkipFirst: false,
				MsgAction: prefix + pair,
			}
		}
		if err_ != nil {
			log.Error("broadCast kline fail", zap.String("pair", pair), zap.Error(err_))
		}
	}

	// 处理ws推送的K线数据
	go func() {
		defer func() {
			m.KlineReady = false
			log.Info("watch kline stopped, retry after 3s", zap.String("exg", m.ExgName))
			time.Sleep(time.Millisecond * 3100)
			m.watchKLines(utils.KeysOfMap(m.KlinePairs))
		}()
		for {
			first := <-out
			if first == nil {
				continue
			}
			cache := map[string][]*banexg.Kline{}
			curKey := fmt.Sprintf("%s_%s", first.Symbol, first.TimeFrame)
			cache[curKey] = []*banexg.Kline{&first.Kline}
		readCache:
			for {
				select {
				case val := <-out:
					valKey := fmt.Sprintf("%s_%s", val.Symbol, val.TimeFrame)
					arr, _ := cache[valKey]
					if len(arr) > 0 && arr[len(arr)-1].Time == val.Time {
						arr[len(arr)-1] = &val.Kline
					} else {
						cache[valKey] = append(arr, &val.Kline)
					}
				default:
					break readCache
				}
			}
			for key, arr := range cache {
				handleSubKLines(key, arr)
			}
		}
	}()

	// 交易所ws可能偶发故障，这里定期检查，通过rest获取k线(约1m一次)
	delayMS := int64(20000)
	minMSecs := int64(utils2.TFToSecs("1m") * 1000)
	go func() {
		for {
			time.Sleep(time.Millisecond * 3000)
			curMS := btime.TimeMS()
			curMinuteMS := utils2.AlignTfMSecs(curMS, minMSecs)
			if curMS-curMinuteMS < delayMS {
				continue
			}
			alignMS := utils2.AlignTfMSecs(curMS, tfMSecs)
			adds := make(map[string]int)
			for pair, sta := range m.klineStates {
				if curMinuteMS > sta.ExpectMS+delayMS {
					bars, err := m.exchange.FetchOHLCV(pair, timeFrame, sta.ExpectMS, 0, nil)
					if err != nil {
						log.Warn("FetchOHLCV fail", zap.String("pair", pair), zap.Error(err))
						continue
					}
					if len(bars) > 0 {
						adds[pair] = len(bars)
						key := fmt.Sprintf("%s_%s", pair, timeFrame)
						last := bars[len(bars)-1]
						if last.Time < alignMS {
							// 交易所未返回未完成bar，添加最后一个未完成bar
							bars = append(bars, &banexg.Kline{
								Time:  last.Time + tfMSecs,
								Open:  last.Close,
								High:  last.Close,
								Low:   last.Close,
								Close: last.Close,
							})
						}
						handleSubKLines(key, bars)
					}
				}
			}
			if len(adds) > 0 {
				log.Warn("FetchOHLCV as ws timeout", zap.String("items", utils.MapToStr(adds, true, 0)))
			}
		}
	}()
}

func RunSpider(addr string) *errs.Error {
	server := utils.NewBanServer(addr, "spider")
	Spider = &LiveSpider{
		ServerIO: server,
		miners:   map[string]*Miner{},
	}
	server.InitConn = makeInitConn(Spider)
	go consumeWriteQ(5)
	sess, conn, err := orm.Conn(nil)
	if err != nil {
		return err
	}
	err = sess.PurgeKlineUn()
	if err != nil {
		conn.Release()
		return err
	}
	conn.Release()
	return Spider.RunForever()
}

func (s *LiveSpider) getMiner(exgName, market string) *Miner {
	key := fmt.Sprintf("%s:%s", exgName, market)
	miner, ok := s.miners[key]
	var err *errs.Error
	if !ok {
		miner, err = newMiner(s, exgName, market)
		if err != nil {
			panic(err)
		}
		miner.init()
		err = miner.SubPairs("price")
		if err != nil {
			log.Error("sub prices fail", zap.Error(err))
		}
		s.miners[key] = miner
		log.Info("start miner for", zap.String("e", exgName), zap.String("m", market))
	}
	return miner
}

func makeInitConn(s *LiveSpider) func(*utils.BanConn) {
	return func(c *utils.BanConn) {
		handlePairs := func(data []byte, name string) (*Miner, []string) {
			var arr = make([]string, 0, 8)
			err := utils2.Unmarshal(data, &arr, utils2.JsonNumDefault)
			if err != nil {
				log.Warn("receive invalid pairs", zap.String("n", name),
					zap.String("in", string(data)), zap.Error(err))
				return nil, nil
			}
			if len(arr) < 4 {
				log.Error(name+" receive invalid", zap.Strings("msg", arr))
				return nil, nil
			}
			miner := s.getMiner(arr[0], arr[1])
			return miner, arr[2:]
		}
		c.Listens["watch_pairs"] = func(_ string, data []byte) {
			miner, arr := handlePairs(data, "watch_pairs")
			err := miner.SubPairs(arr[0], arr[1:]...)
			if err != nil {
				log.Error("spider.sub_pairs fail", zap.Error(err))
			}
		}
		c.Listens["unwatch_pairs"] = func(_ string, data []byte) {
			miner, arr := handlePairs(data, "unwatch_pairs")
			err := miner.UnSubPairs(arr[0], arr[1:]...)
			if err != nil {
				log.Error("spider.unsub_pairs fail", zap.Error(err))
			}
		}
	}
}
