package data

import (
	"fmt"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

type KLineWatcher struct {
	*utils.ClientIO
	jobs     map[string]PairTFCache
	initMsgs []*utils.IOMsg
}

type WatchJob struct {
	Symbol    string
	TimeFrame string
	Since     int64
}

func NewKlineWatcher(addr string) (*KLineWatcher, *errs.Error) {
	client, err := utils.NewClientIO(addr)
	if err != nil {
		return nil, err
	}
	res := &KLineWatcher{
		ClientIO: client,
		jobs:     make(map[string]PairTFCache),
	}
	res.Listens["uohlcv"] = res.onSpiderBar
	res.Listens["ohlcv"] = res.onSpiderBar
	res.Listens["update_price"] = res.onPriceUpdate
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
		w.jobs[jobKey] = PairTFCache{TimeFrame: j.TimeFrame, TFSecs: tfSecs, NextMS: j.Since}
	}
	msg := &utils.IOMsg{Action: "subscribe", Data: tags}
	err := w.WriteMsg(msg)
	if err != nil {
		return err
	}
	w.initMsgs = append(w.initMsgs, msg)
	if minTfSecs < 60 && banexg.IsContract(marketType) && jobType == "ohlcv" {
		//合约市场不支持1m以下的ohlcv，使用ws监听交易归集
		jobType = "trade"
	}
	args := append([]string{exgName, marketType, jobType}, pairs...)
	msg = &utils.IOMsg{Action: "watch_pairs", Data: args}
	err = w.WriteMsg(msg)
	if err != nil {
		return err
	}
	w.initMsgs = append(w.initMsgs, msg)
	return nil
}

func (w *KLineWatcher) onSpiderBar(key string, data interface{}) {
	msgText, _ := sonic.MarshalString(data)
	log.Info("receive ohlcv", zap.String("k", key), zap.String("t", msgText))
}

func (w *KLineWatcher) onPriceUpdate(key string, data interface{}) {

}

func (w *KLineWatcher) onTrades(key string, data interface{}) {

}

func (w *KLineWatcher) onBook(key string, data interface{}) {

}
