package data

import (
	"fmt"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/bytedance/sonic"
	"go.uber.org/zap"
	"strings"
)

type KLineWatcher struct {
	*utils.ClientIO
	jobs       map[string]*PairTFCache
	initMsgs   []*utils.IOMsg
	OnKLineMsg func(msg *KLineMsg) // 收到爬虫K线消息
	OnTrade    func(exgName, market string, trade *banexg.Trade)
}

type WatchJob struct {
	Symbol    string
	TimeFrame string
	Since     int64
}

func (j *PairTFCache) getFinishes(ohlcvs []*banexg.Kline, lastFinish bool) []*banexg.Kline {
	if len(ohlcvs) == 0 {
		return ohlcvs
	}
	j.WaitBar = nil
	if !lastFinish {
		j.WaitBar = ohlcvs[len(ohlcvs)-1]
		ohlcvs = ohlcvs[:len(ohlcvs)-1]
	}
	return ohlcvs
}

func NewKlineWatcher(addr string) (*KLineWatcher, *errs.Error) {
	client, err := utils.NewClientIO(addr)
	if err != nil {
		return nil, err
	}
	res := &KLineWatcher{
		ClientIO: client,
		jobs:     make(map[string]*PairTFCache),
	}
	res.Listens["uohlcv"] = res.onSpiderBar
	res.Listens["ohlcv"] = res.onSpiderBar
	res.Listens["price"] = res.onPriceUpdate
	res.Listens["trade"] = res.onTrades
	res.Listens["book"] = res.onBook
	res.ReInitConn = func() {
		if len(res.initMsgs) == 0 {
			return
		}
		for _, msg := range res.initMsgs {
			err := res.WriteMsg(msg)
			if err != nil {
				msgText, _ := sonic.MarshalString(msg)
				log.Error("re init conn fail", zap.String("msg", msgText))
				return
			}
		}
	}
	return res, nil
}

func (w *KLineWatcher) getPrefixs(exgName, marketType, jobType string) []string {
	exgMarket := fmt.Sprintf("%s_%s", exgName, marketType)
	prefixs := make([]string, 0, 2)
	if jobType == "ws" {
		prefixs = append(prefixs, "trade_"+exgMarket, "book_"+exgMarket)
	} else {
		prefixs = append(prefixs, jobType+"_"+exgMarket)
	}
	return prefixs
}

/*
WatchJobs
从爬虫订阅数据。ohlcv/uohlcv/ws/trade/book
*/
func (w *KLineWatcher) WatchJobs(exgName, marketType, jobType string, jobs ...WatchJob) *errs.Error {
	prefixs := w.getPrefixs(exgName, marketType, jobType)
	tags := make([]string, 0, len(jobs))
	pairs := make([]string, 0, len(jobs))
	minTfSecs := 300
	exchange, err := exg.GetWith(exgName, marketType, "")
	if err != nil {
		return err
	}
	exgID := exchange.Info().ID
	for _, j := range jobs {
		jobKey := fmt.Sprintf("%s_%s", j.Symbol, jobType)
		if _, ok := w.jobs[jobKey]; ok {
			continue
		}
		tfSecs := utils.TFToSecs(j.TimeFrame)
		minTfSecs = min(minTfSecs, tfSecs)
		for _, p := range prefixs {
			tags = append(tags, p+"_"+j.Symbol)
		}
		pairs = append(pairs, j.Symbol)
		w.jobs[jobKey] = &PairTFCache{TimeFrame: j.TimeFrame, TFSecs: tfSecs, NextMS: j.Since,
			AlignOffMS: int64(exg.GetAlignOff(exgID, tfSecs) * 1000)}
		// 尽早启动延迟监听
		core.SetPairMs(j.Symbol, j.Since, int64(tfSecs*1000))
	}
	err = w.SendMsg("subscribe", tags)
	if err != nil {
		return err
	}
	if minTfSecs < 60 && banexg.IsContract(marketType) && jobType == "ohlcv" {
		//合约市场不支持1m以下的ohlcv，使用ws监听交易归集
		jobType = "trade"
	}
	args := append([]string{exgName, marketType, jobType}, pairs...)
	return w.SendMsg("watch_pairs", args)
}

func (w *KLineWatcher) SendMsg(action string, data interface{}) *errs.Error {
	msg := &utils.IOMsg{Action: action, Data: data}
	err := w.WriteMsg(msg)
	if err != nil {
		return err
	}
	w.initMsgs = append(w.initMsgs, msg)
	return nil
}

func (w *KLineWatcher) UnWatchJobs(exgName, marketType, jobType string, pairs []string) *errs.Error {
	prefixs := w.getPrefixs(exgName, marketType, jobType)
	tags := make([]string, 0, len(prefixs)*len(pairs))
	for _, pair := range pairs {
		for _, prefix := range prefixs {
			tags = append(tags, fmt.Sprintf("%s_%s", prefix, pair))
		}
		jobKey := fmt.Sprintf("%s_%s", pair, jobType)
		delete(w.jobs, jobKey)
		delete(core.PairCopiedMs, pair)
	}
	return w.WriteMsg(&utils.IOMsg{Action: "unsubscribe", Data: tags})
}

func (w *KLineWatcher) onSpiderBar(key string, data interface{}) {
	if w.OnKLineMsg == nil {
		log.Debug("receive spider bar skipped", zap.String("key", key))
		return
	}
	parts := strings.Split(key, "_")
	msgType, exgName, market, pair := parts[0], parts[1], parts[2], parts[3]
	job, ok := w.jobs[fmt.Sprintf("%s_%s", pair, msgType)]
	if !ok {
		// 未监听，忽略
		log.Debug("receive spider bar ignored", zap.String("key", key))
		return
	}
	var bars NotifyKLines
	if !utils.DecodeMsgData(data, &bars, "onSpiderBar") {
		log.Debug("receive spider bar decode fail", zap.String("key", key))
		return
	}
	if len(bars.Arr) == 0 {
		log.Debug("receive spider bar empty", zap.String("key", key))
		return
	}
	// 更新收到的时间戳
	lastBarMS := bars.Arr[len(bars.Arr)-1].Time
	tfMSecs := int64(bars.TFSecs * 1000)
	nextBarMS := lastBarMS + tfMSecs
	core.SetPairMs(pair, nextBarMS, tfMSecs)
	var msg = &KLineMsg{
		NotifyKLines: bars,
		ExgName:      exgName,
		Market:       market,
		Pair:         pair,
	}
	logFields := []zap.Field{zap.String("key", key), zap.Int("num", len(bars.Arr)),
		zap.Int64("nextMS", nextBarMS)}
	if msgType == "uohlcv" {
		log.Debug("receive spider bar uohlcv", logFields...)
		w.OnKLineMsg(msg)
		return
	}
	log.Debug("receive spider bar ohlcv", logFields...)
	olds, err := job.fillLacks(pair, bars.TFSecs, bars.Arr[0].Time, nextBarMS)
	if err != nil {
		log.Error("fillLacks fail", zap.String("pair", pair), zap.Error(err))
		return
	}
	// 归集更新指定的周期
	var finishes []*banexg.Kline
	if bars.TFSecs < job.TFSecs {
		//和旧的bar_row合并更新，判断是否有完成的bar
		if job.WaitBar != nil {
			olds = append(olds, job.WaitBar)
		}
		jobMSecs := int64(job.TFSecs * 1000)
		finishes = job.getFinishes(utils.BuildOHLCV(bars.Arr, jobMSecs, 0, olds, tfMSecs, job.AlignOffMS))
	} else {
		finishes = bars.Arr
	}
	if len(finishes) > 0 {
		msg.TFSecs = job.TFSecs
		msg.Interval = job.TFSecs
		msg.Arr = finishes
		w.OnKLineMsg(msg)
	}
}

func (w *KLineWatcher) onPriceUpdate(key string, data interface{}) {
	parts := strings.Split(key, "_")
	exgName, market := parts[1], parts[2]
	if exgName != core.ExgName || market != core.Market {
		return
	}
	var msg map[string]float64
	if !utils.DecodeMsgData(data, &msg, "onPriceUpdate") {
		return
	}
	for pair, price := range msg {
		core.SetPrice(pair, price)
	}
}

func (w *KLineWatcher) onTrades(key string, data interface{}) {
	if w.OnTrade == nil {
		return
	}
	parts := strings.Split(key, "_")
	exgName, market := parts[1], parts[2]
	var trade banexg.Trade
	if !utils.DecodeMsgData(data, &trade, "onTrades") {
		return
	}
	w.OnTrade(exgName, market, &trade)
}

func (w *KLineWatcher) onBook(key string, data interface{}) {
	parts := strings.Split(key, "_")
	msgType, exgName, market, pair := parts[0], parts[1], parts[2], parts[3]
	if exgName != core.ExgName || market != core.Market {
		return
	}
	_, ok := w.jobs[fmt.Sprintf("%s_%s", pair, msgType)]
	if !ok {
		// 未监听，忽略
		return
	}
	var book banexg.OrderBook
	if !utils.DecodeMsgData(data, &book, "onBook") {
		return
	}
	if book.Symbol == "" {
		return
	}
	core.OdBooks[pair] = &book
}
