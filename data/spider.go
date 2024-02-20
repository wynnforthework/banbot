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
	"go.uber.org/zap"
	"strings"
	"time"
)

type NotifyKLines struct {
	TFSecs   int
	Interval int // 推送更新间隔, <= TFSecs
	Arr      []*banexg.Kline
}

type KLineMsg struct {
	NotifyKLines
	ExgName string // 交易所名称
	Market  string // 市场
	Pair    string //币种
}

/*
getCheckInterval
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

/** *******************************  爬虫部分   ****************************
 */
var (
	initSids = map[int32]bool{}
	writeQ   = make(chan SaveKline, 1000)
)

type SaveKline struct {
	Sid       int32
	TimeFrame string
	Arr       []*banexg.Kline
	SkipFirst bool
}

func fillPrevHole(sess *orm.Queries, save *SaveKline) *errs.Error {
	initSids[save.Sid] = true
	if save.SkipFirst {
		save.Arr = save.Arr[1:]
	}
	if len(save.Arr) == 0 {
		return nil
	}
	exs := orm.GetSymbolByID(save.Sid)
	tfMSecs := int64(utils.TFToSecs(save.TimeFrame) * 1000)
	fetchEndMS := save.Arr[0].Time

	var err *errs.Error
	_, endMS := sess.GetKlineRange(save.Sid, save.TimeFrame)
	if endMS == 0 || fetchEndMS <= endMS {
		// 新的币无历史数据、或当前bar和已插入数据连续，直接插入后续新bar即可
		return nil
	}
	tryCount := 0
	log.Info("start first fetch", zap.String("pair", exs.Symbol), zap.Int64("s", endMS), zap.Int64("e", fetchEndMS))
	exchange, err := exg.GetWith(exs.Exchange, exs.Market, "")
	if err != nil {
		return err
	}
	var saveNum int
	for tryCount <= 5 {
		tryCount += 1
		saveNum, err = sess.DownOHLCV2DB(exchange, exs, save.TimeFrame, endMS, fetchEndMS, nil)
		if err != nil {
			return err
		}
		saveBars, err := sess.QueryOHLCV(exs.ID, save.TimeFrame, 0, fetchEndMS, 10, false)
		if err != nil {
			return err
		}
		var lastMS = int64(0)
		if len(saveBars) > 0 {
			lastMS = saveBars[len(saveBars)-1].Time
		}
		if lastMS+tfMSecs == fetchEndMS {
			break
		} else {
			//如果未成功获取最新的bar，等待2s重试（1m刚结束时请求ohlcv可能取不到）
			log.Info("query first fail, wait 2s", zap.String("pair", exs.Symbol),
				zap.Int("ins", saveNum), zap.Int64("last", lastMS))
			time.Sleep(time.Second * 2)
		}
	}
	log.Info("first fetch ok", zap.String("pair", exs.Symbol), zap.Int64("s", endMS),
		zap.Int64("e", fetchEndMS))
	return nil
}

func consumeWriteQ(workNum int) {
	guard := make(chan struct{}, workNum)
	defer close(guard)
	for save := range writeQ {
		guard <- struct{}{}
		go func(job *SaveKline) {
			ctx := context.Background()
			sess, conn, err := orm.Conn(ctx)
			if err == nil {
				defer conn.Release()
				if _, ok := initSids[job.Sid]; !ok {
					err = fillPrevHole(sess, job)
				}
				if err == nil {
					_, err = sess.InsertKLinesAuto(job.TimeFrame, job.Sid, job.Arr)
				}
			}
			if err != nil {
				log.Error("consumeWriteQ: fail", zap.Int32("sid", job.Sid), zap.Error(err))
			}
			<-guard
		}(&save)
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
		spider:     spider,
		ExgName:    exgName,
		Market:     market,
		exchange:   exchange,
		Fetchs:     map[string]*FetchJob{},
		KlinePairs: map[string]bool{},
		TradePairs: map[string]bool{},
		BookPairs:  map[string]bool{},
	}, nil
}

func (m *Miner) init() {
	_, err := m.exchange.LoadMarkets(false, nil)
	if err != nil {
		log.Error("load markets for miner fail", zap.String("exg", m.ExgName), zap.Error(err))
	}
}

func (m *Miner) SubPairs(jobType string, pairs ...string) *errs.Error {
	valids, _ := m.exchange.CheckSymbols(pairs...)
	if len(valids) == 0 {
		return nil
	}
	ensures := make([]*orm.ExSymbol, 0, len(valids))
	for _, p := range pairs {
		ensures = append(ensures, &orm.ExSymbol{
			Exchange: m.ExgName,
			Market:   m.Market,
			Symbol:   p,
		})
	}
	err := orm.EnsureSymbols(ensures)
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
			log.Info("watch trades stopped", zap.String("exg", m.ExgName))
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
	out, err := m.exchange.WatchMarkPrices(nil, nil)
	if err != nil {
		log.Error("watch prices fail", zap.String("exg", m.ExgName), zap.Error(err))
		return
	}
	m.IsWatchPrice = true
	log.Info("start watch prices", zap.String("exg", m.ExgName))
	prefix := fmt.Sprintf("price_%s_%s_", m.ExgName, m.Market)

	go func() {
		defer func() {
			m.IsWatchPrice = false
			log.Info("watch prices stopped", zap.String("exg", m.ExgName))
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
			log.Info("watch odBook stopped", zap.String("exg", m.ExgName))
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
	PrevBar    *banexg.Kline
}

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
	for p := range m.KlinePairs {
		jobs = append(jobs, [2]string{p, timeFrame})
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
	tfSecs := utils.TFToSecs(timeFrame)

	// pair_tf to state map
	subStateMap := map[string]*SubKLineState{}
	getState := func(key string) *SubKLineState {
		if val, ok := subStateMap[key]; ok {
			return val
		}
		pair := strings.Split(key, "_")[0]
		exs, err := orm.GetExSymbol(m.exchange, pair)
		if err != nil {
			log.Error("save kline fail", zap.String("key", key), zap.Error(err))
			subStateMap[key] = nil
			return nil
		}
		res := &SubKLineState{
			Sid:        exs.ID,
			NextNotify: 0,
			PrevBar:    nil,
		}
		subStateMap[key] = res
		return res
	}

	// 收到K线，发送到机器人，保存到数据库
	handleSubKLines := func(key string, arr []*banexg.Kline) {
		parts := strings.Split(key, "_")
		pair, curTF := parts[0], parts[1]
		state := getState(key)
		if state == nil {
			return
		}
		curTS := btime.Time()
		var err_ *errs.Error
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
			// 1s最多发送1次k线消息
			state.NextNotify = curTS + 0.9
		}
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
		}
		if len(finishes) > 0 {
			// 有已完成的k线
			err_ = m.spider.Broadcast(&utils.IOMsg{
				Action: prefix + pair,
				Data: NotifyKLines{
					TFSecs:   tfSecs,
					Interval: tfSecs,
					Arr:      finishes,
				},
			})
			writeQ <- SaveKline{
				Sid:       state.Sid,
				TimeFrame: curTF,
				Arr:       finishes,
				SkipFirst: false,
			}
		}
		if err_ != nil {
			log.Error("broadCast kline fail", zap.String("pair", pair), zap.Error(err_))
		}
	}

	go func() {
		defer func() {
			m.KlineReady = false
			log.Info("watch kline stopped", zap.String("exg", m.ExgName))
		}()
		for {
			first := <-out
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
}

func RunSpider(addr string) *errs.Error {
	server := utils.NewBanServer(addr, "spider")
	spider := &LiveSpider{
		ServerIO: server,
		miners:   map[string]*Miner{},
	}
	server.InitConn = makeInitConn(spider)
	go consumeWriteQ(5)
	sess, conn, err := orm.Conn(nil)
	if err != nil {
		return err
	}
	err = sess.PurgeKlineUn()
	if err != nil {
		return err
	}
	conn.Release()
	return spider.RunForever()
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
		handlePairs := func(data interface{}, name string) (*Miner, []string) {
			var arr = make([]string, 0, 8)
			if !utils.DecodeMsgData(data, &arr, name) {
				return nil, nil
			}
			if len(arr) < 4 {
				log.Error(name+" receive invalid", zap.Strings("msg", arr))
				return nil, nil
			}
			miner := s.getMiner(arr[0], arr[1])
			return miner, arr[2:]
		}
		c.Listens["watch_pairs"] = func(_ string, data interface{}) {
			miner, arr := handlePairs(data, "watch_pairs")
			err := miner.SubPairs(arr[0], arr[1:]...)
			if err != nil {
				log.Error("spider.sub_pairs fail", zap.Error(err))
			}
		}
		c.Listens["unwatch_pairs"] = func(_ string, data interface{}) {
			miner, arr := handlePairs(data, "unwatch_pairs")
			err := miner.UnSubPairs(arr[0], arr[1:]...)
			if err != nil {
				log.Error("spider.unsub_pairs fail", zap.Error(err))
			}
		}
	}
}
