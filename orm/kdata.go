package orm

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"sync"
)

/*
FetchApiOHLCV
按给定时间段下载交易对的K线数据。
如果需要从后往前下载，应该使startMS>endMS
*/
func FetchApiOHLCV(ctx context.Context, exchange banexg.BanExchange, pair, timeFrame string, startMS, endMS int64, out chan []*banexg.Kline) *errs.Error {
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	if startMS < 1000000000000 {
		panic(fmt.Sprintf("startMS should be milli seconds, cur: %v", startMS))
	}
	dirt := 1 // 1从前往后下载， -1从后往前下载
	if startMS > endMS {
		startMS, endMS = endMS, startMS
		dirt = -1
	}
	maxBarEndMS := (endMS/tfMSecs - 1) * tfMSecs
	if startMS > maxBarEndMS {
		return nil
	}
	fetchNum := int((endMS - startMS) / tfMSecs)
	batchSize := min(1000, fetchNum+5)
	since := startMS
	if dirt == -1 {
		// 从后往前下载
		since = endMS - int64(batchSize)*tfMSecs
	}
	nextStart := func(start, stop int64) int64 {
		if dirt == 1 {
			if stop >= endMS {
				return 0
			}
			return stop
		} else {
			// 从后往前下载
			if start <= startMS {
				return 0
			}
			return max(start-int64(batchSize)*tfMSecs, startMS)
		}
	}
	for since > 0 {
		data, err := exchange.FetchOHLCV(pair, timeFrame, since, batchSize, nil)
		if err != nil {
			return err
		}
		if len(data) == 0 {
			since = nextStart(since, since+int64(batchSize)*tfMSecs)
			continue
		}
		lastEndMS := data[len(data)-1].Time
		since = nextStart(data[0].Time, lastEndMS+tfMSecs)
		if lastEndMS > maxBarEndMS {
			endPos := len(data) - 1
			for endPos >= 0 && data[endPos].Time > maxBarEndMS {
				endPos -= 1
			}
			data = data[:endPos+1]
		}
		if len(data) > 0 {
			select {
			case <-ctx.Done():
				return nil
			case out <- data:
			}
		}
	}
	return nil
}

func DownOHLCV2Db(sess *Queries, exchange banexg.BanExchange, exs *ExSymbol, timeFrame string, startMS, endMS int64) *errs.Error {
	startMS = exs.GetValidStart(startMS)
	chanDown := make(chan core.DownRange, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	oldStart, oldEnd := sess.GetKlineRange(exs.ID, timeFrame)
	if oldStart == 0 {
		// 数据不存在，下载全部区间
		chanDown <- core.DownRange{Start: startMS, End: endMS}
	} else if oldStart <= startMS && endMS <= oldEnd {
		// 完全处于已下载的区间，无需下载
		close(chanDown)
		return nil
	} else if startMS < oldStart && endMS > oldEnd {
		// 范围超过已下载区间前后范围
		chanDown <- core.DownRange{Start: startMS, End: oldStart, Reverse: true}
		chanDown <- core.DownRange{Start: oldEnd, End: endMS}
	} else if startMS < oldStart {
		// 前部超过已下载范围，只下载前面
		chanDown <- core.DownRange{Start: startMS, End: oldStart, Reverse: true}
	} else if endMS > oldEnd {
		// 后部超过已下载范围，只下载后面
		chanDown <- core.DownRange{Start: oldEnd, End: endMS}
	}
	close(chanDown)
	chanKline := make(chan []*banexg.Kline, 1000)
	var wg sync.WaitGroup
	wg.Add(2)
	var err *errs.Error

	// 启动一个goroutine下载K线，写入到chanDown
	go func() {
		defer wg.Done()
		defer close(chanKline)
		for {
			select {
			case <-ctx.Done():
				return
			case job, ok := <-chanDown:
				if !ok {
					return
				}
				start, stop := job.Start, job.End
				if job.Reverse {
					start, stop = job.End, job.Start
				}
				err = FetchApiOHLCV(ctx, exchange, exs.Symbol, timeFrame, start, stop, chanKline)
				if err != nil {
					return
				}
			}
		}
	}()
	// 启动一个goroutine将K线保存到数据库
	go func() {
		defer wg.Done()
		tblName := "kline_" + timeFrame
		for {
			select {
			case <-ctx.Done():
				return
			case batch, ok := <-chanKline:
				if !ok {
					return
				}
				var adds = make([]*KlineSid, len(batch))
				for i, v := range batch {
					adds[i] = &KlineSid{
						Kline: *v,
						Sid:   exs.ID,
					}
				}
				_, err_ := sess.InsertKLines(tblName, adds)
				if err_ != nil {
					log.Error("insert kline fail", zap.Error(err_))
					err = errs.New(core.ErrDbExecFail, err_)
					cancel()
					return
				}
			}
		}
	}()

	wg.Wait()
	if err != nil {
		return err
	}
	return sess.UpdateKRange(exs.ID, timeFrame, startMS, endMS)
}

/*
AutoFetchOHLCV

	获取给定交易对，给定时间维度，给定范围的K线数据。
	先尝试从本地读取，不存在时从交易所下载，然后返回。
*/
func AutoFetchOHLCV(exchange banexg.BanExchange, exs *ExSymbol, timeFrame string, startMS, endMS int64,
	limit int, withUnFinish bool) ([]*banexg.Kline, *errs.Error) {
	startMS, endMS = parseDownArgs(timeFrame, startMS, endMS, limit, withUnFinish)
	downTF, err := GetDownTF(timeFrame)
	if err != nil {
		return nil, err
	}
	sess, conn, err := Conn(nil)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	err = DownOHLCV2Db(sess, exchange, exs, downTF, startMS, endMS)
	if err != nil {
		return nil, err
	}
	return sess.QueryOHLCV(exs.ID, timeFrame, startMS, endMS, limit, withUnFinish)
}

/*
FastBulkOHLCV
快速批量获取K线。先下载所有需要的币种，然后批量查询再分组返回。

	适用于币种较多，且需要的开始结束时间一致，且大部分已下载的情况。
*/
func FastBulkOHLCV(exchange banexg.BanExchange, symbols []string, timeFrame string,
	startMS, endMS int64, limit int, handler func(string, []*banexg.Kline)) *errs.Error {
	startMS, endMS = parseDownArgs(timeFrame, startMS, endMS, limit, false)
	downTF, err := GetDownTF(timeFrame)
	if err != nil {
		return err
	}
	guard := make(chan struct{}, core.DownOHLCVParallel)
	var wg sync.WaitGroup
	defer wg.Wait()
	exgName := exchange.GetID()
	var market *banexg.Market
	var exs *ExSymbol
	var retErr *errs.Error
	sidMap := map[int32]string{}
	sess, conn, err := Conn(nil)
	if err != nil {
		return err
	}
	defer conn.Release()
	for _, symbol := range symbols {
		market, err = exchange.GetMarket(symbol)
		if err != nil {
			return err
		}
		exs, err = GetSymbol(exgName, market.Type, symbol)
		if err != nil {
			return err
		}
		// 如果达到并发限制，这里会阻塞等待
		guard <- struct{}{}
		if retErr != nil {
			// 下载出错，中断返回
			break
		}
		wg.Add(1)
		sidMap[exs.ID] = symbol
		go func(exs_ *ExSymbol) {
			defer wg.Done()
			retErr = DownOHLCV2Db(sess, exchange, exs_, downTF, startMS, endMS)
			// 完成一个任务，从chan弹出一个
			<-guard
		}(exs)
	}
	if retErr != nil {
		return retErr
	}
	if handler == nil {
		return nil
	}
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	itemNum := (endMS - startMS) / tfMSecs
	if itemNum < 1000 {
		sidArr := utils.KeysOfMap(sidMap)
		bulkHandler := func(sid int32, klines []*banexg.Kline) {
			symbol, ok := sidMap[sid]
			if !ok {
				return
			}
			handler(symbol, klines)
		}
		return sess.QueryOHLCVBatch(sidArr, timeFrame, startMS, endMS, limit, bulkHandler)
	}
	// 单个数量过多，逐个查询
	for sid, symbol := range sidMap {
		kline, err := sess.QueryOHLCV(sid, timeFrame, startMS, endMS, limit, false)
		if err != nil {
			return err
		}
		handler(symbol, kline)
	}
	return nil
}

func parseDownArgs(timeFrame string, startMS, endMS int64, limit int, withUnFinish bool) (int64, int64) {
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	if startMS > 0 {
		fixStartMS := utils.AlignTfMSecs(startMS, tfMSecs)
		if startMS > fixStartMS {
			startMS = fixStartMS + tfMSecs
		}
		if limit > 0 && endMS == 0 {
			endMS = startMS + tfMSecs*int64(limit)
		}
	}
	if endMS == 0 {
		endMS = btime.TimeMS()
	}
	alignEndMS := utils.AlignTfMSecs(endMS, tfMSecs)
	if withUnFinish && endMS%tfMSecs > 0 {
		alignEndMS += tfMSecs
	}
	endMS = alignEndMS
	if startMS == 0 {
		startMS = endMS - tfMSecs*int64(limit)
	}
	return startMS, endMS
}
