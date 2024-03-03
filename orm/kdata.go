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
	"github.com/schollz/progressbar/v3"
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
	fetchNum := int((endMS - startMS) / tfMSecs)
	if fetchNum == 0 {
		return nil
	}
	rangeMSecs := int64(min(core.KBatchSize, fetchNum+5)) * tfMSecs
	nextRange := func(start, stop int64) (int64, int64) {
		if dirt == 1 {
			if stop >= endMS {
				return 0, 0
			}
			return stop, min(endMS, stop+rangeMSecs)
		} else {
			// 从后往前下载
			if start <= startMS {
				return 0, 0
			}
			return max(startMS, start-rangeMSecs), start
		}
	}
	since, until := nextRange(startMS, startMS)
	if dirt == -1 {
		// 从后往前下载
		since, until = nextRange(endMS, endMS)
	}
	for since > 0 && until > since {
		curSize := int((until - since) / tfMSecs)
		data, err := exchange.FetchOHLCV(pair, timeFrame, since, curSize, nil)
		if err != nil {
			return err
		}
		// 移除末尾超出范围的K线
		for len(data) > 0 && data[len(data)-1].Time >= until {
			data = data[:len(data)-1]
		}
		since, until = nextRange(since, until)
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

/*
DownOHLCV2DB
下载K线到数据库，应在事务中调用此方法，否则查询更新相关数据会有错误
*/
func (q *Queries) DownOHLCV2DB(exchange banexg.BanExchange, exs *ExSymbol, timeFrame string, startMS, endMS int64,
	stepCB func(num int)) (int, *errs.Error) {
	startMS = exs.GetValidStart(startMS)
	oldStart, oldEnd := q.GetKlineRange(exs.ID, timeFrame)
	return downOHLCV2DBRange(exchange, exs, timeFrame, startMS, endMS, oldStart, oldEnd, stepCB)
}

/*
downOHLCV2DBRange
此函数会用于多线程下载，一个数据库会话只能用于一个线程，所以不能传入Queries
stepCB 用于更新进度，总值固定1000，避免内部下载区间大于传入区间
*/
func downOHLCV2DBRange(exchange banexg.BanExchange, exs *ExSymbol, timeFrame string, startMS, endMS,
	oldStart, oldEnd int64, stepCB func(num int)) (int, *errs.Error) {
	if oldStart <= startMS && endMS <= oldEnd || startMS <= exs.ListMs && endMS <= exs.ListMs {
		// 完全处于已下载的区间 或 下载区间小于上市时间，无需下载
		if stepCB != nil {
			stepCB(core.StepTotal)
		}
		return 0, nil
	}
	tfSecs := utils.TFToSecs(timeFrame)
	var totalNum int
	chanDown := make(chan core.DownRange, 10)
	if oldStart == 0 {
		// 数据不存在，下载全部区间
		chanDown <- core.DownRange{Start: startMS, End: endMS}
		totalNum = int((endMS-startMS)/1000) / tfSecs
	} else {
		if endMS > oldEnd {
			// 后部超过已下载范围，下载后面
			chanDown <- core.DownRange{Start: oldEnd, End: endMS}
			totalNum += int((endMS-oldEnd)/1000) / tfSecs
		}
		if startMS < oldStart {
			// 前部超过已下载范围，下载前面
			chanDown <- core.DownRange{Start: startMS, End: oldStart, Reverse: true}
			totalNum += int((oldStart-startMS)/1000) / tfSecs
		}
	}
	if stepCB == nil && totalNum > 10000 {
		var pBar = progressbar.Default(int64(core.StepTotal))
		defer pBar.Close()
		var m sync.Mutex
		stepCB = func(num int) {
			m.Lock()
			defer m.Unlock()
			err_ := pBar.Add(num)
			if err_ != nil {
				log.Error("update pBar fail", zap.Error(err_))
			}
		}
	}
	doneNum := 0
	progressNum := 0
	curStep := func(curNum int) {
		if stepCB == nil || curNum <= 0 {
			return
		}
		doneNum += curNum
		curProgress := min(core.StepTotal, doneNum*core.StepTotal/totalNum)
		addNum := curProgress - progressNum
		if addNum > 0 {
			stepCB(addNum)
		}
		progressNum = curProgress
	}
	close(chanDown)
	chanKline := make(chan []*banexg.Kline, 1000)
	var wg sync.WaitGroup
	wg.Add(2)
	var outErr *errs.Error
	saveNum := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动一个goroutine下载K线，写入到chanDown
	go func() {
		defer func() {
			wg.Done()
			close(chanKline)
		}()
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
				barNum := int((job.End-job.Start)/1000) / tfSecs
				if barNum > 10000 {
					startText := btime.ToDateStr(job.Start, "")
					endText := btime.ToDateStr(job.End, "")
					log.Info(fmt.Sprintf("fetch %s %s  %s - %s, num: %d", exs.Symbol, timeFrame, startText, endText, barNum))
				}
				err := FetchApiOHLCV(ctx, exchange, exs.Symbol, timeFrame, start, stop, chanKline)
				if err != nil {
					outErr = err
					cancel()
					return
				}
			}
		}
	}()
	// 启动一个goroutine将K线保存到数据库
	go func() {
		defer wg.Done()
		var num int64
		sess, conn, err := Conn(nil)
		if err != nil {
			log.Error("get db sess fail to save klines", zap.Error(err))
			cancel()
			return
		}
		defer conn.Release()
		for {
			select {
			case <-ctx.Done():
				return
			case batch, ok := <-chanKline:
				if !ok {
					return
				}
				num, err = sess.InsertKLines(timeFrame, exs.ID, batch)
				if err != nil {
					outErr = err
					cancel()
					return
				} else {
					curNum := int(num)
					curStep(curNum)
					saveNum += curNum
				}
			}
		}
	}()

	wg.Wait()
	curStep(totalNum - doneNum)
	if outErr != nil {
		return saveNum, outErr
	}
	sess, conn, err := Conn(nil)
	if err != nil {
		return saveNum, err
	}
	defer conn.Release()
	err = sess.UpdateKRange(exs.ID, timeFrame, startMS, endMS, nil)
	return saveNum, err
}

/*
AutoFetchOHLCV

	获取给定交易对，给定时间维度，给定范围的K线数据。
	先尝试从本地读取，不存在时从交易所下载，然后返回。
*/
func AutoFetchOHLCV(exchange banexg.BanExchange, exs *ExSymbol, timeFrame string, startMS, endMS int64,
	limit int, withUnFinish bool) ([]*banexg.Kline, *errs.Error) {
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	startMS, endMS = parseDownArgs(tfMSecs, startMS, endMS, limit, withUnFinish)
	downTF, err := GetDownTF(timeFrame)
	if err != nil {
		return nil, err
	}
	sess, conn, err := Conn(nil)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	_, err = sess.DownOHLCV2DB(exchange, exs, downTF, startMS, endMS, nil)
	if err != nil {
		return nil, err
	}
	return sess.QueryOHLCV(exs.ID, timeFrame, startMS, endMS, limit, withUnFinish)
}

/*
BulkDownOHLCV
批量同时下载K线
*/
func BulkDownOHLCV(exchange banexg.BanExchange, exsList map[int32]*ExSymbol, timeFrame string, startMS, endMS int64, limit int) *errs.Error {
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	startMS, endMS = parseDownArgs(tfMSecs, startMS, endMS, limit, false)
	downTF, err := GetDownTF(timeFrame)
	if err != nil {
		return err
	}
	guard := make(chan struct{}, core.DownOHLCVParallel)
	var retErr *errs.Error
	var wg sync.WaitGroup
	defer wg.Wait()
	barNum := int((endMS - startMS) / tfMSecs)
	startText := btime.ToDateStr(startMS, "")
	endText := btime.ToDateStr(endMS, "")
	log.Info(fmt.Sprintf("bulk down %s %d pairs %s-%s, len:%d\n", timeFrame, len(exsList), startText, endText, barNum))
	var pBar = progressbar.Default(int64(len(exsList) * core.StepTotal))
	defer pBar.Close()
	sess, conn, err := Conn(nil)
	if err != nil {
		return err
	}
	var m sync.Mutex
	downStep := func(num int) {
		m.Lock()
		defer m.Unlock()
		err_ := pBar.Add(num)
		if err_ != nil {
			log.Error("add pBar fail", zap.Error(err_))
		}
	}
	sidList := utils.KeysOfMap(exsList)
	// 这里应该使用更小的downTF
	kRanges := sess.GetKlineRanges(sidList, downTF)
	conn.Release()
	for _, exs := range exsList {
		// 如果达到并发限制，这里会阻塞等待
		guard <- struct{}{}
		if retErr != nil {
			// 下载出错，中断返回
			break
		}
		wg.Add(1)
		go func(exs_ *ExSymbol) {
			defer func() {
				// 完成一个任务，从chan弹出一个
				<-guard
				wg.Done()
			}()
			var oldStart, oldEnd = int64(0), int64(0)
			if krange, ok := kRanges[exs_.ID]; ok {
				oldStart, oldEnd = krange[0], krange[1]
			}
			_, retErr = downOHLCV2DBRange(exchange, exs_, downTF, startMS, endMS, oldStart, oldEnd, downStep)
		}(exs)
	}
	wg.Wait()
	return retErr
}

/*
FastBulkOHLCV
快速批量获取K线。先下载所有需要的币种，然后批量查询再分组返回。

	适用于币种较多，且需要的开始结束时间一致，且大部分已下载的情况。
*/
func FastBulkOHLCV(exchange banexg.BanExchange, symbols []string, timeFrame string,
	startMS, endMS int64, limit int, handler func(string, string, []*banexg.Kline)) *errs.Error {
	var exsMap = make(map[int32]*ExSymbol)
	for _, pair := range symbols {
		exs, err := GetExSymbol(exchange, pair)
		if err != nil {
			return err
		}
		exsMap[exs.ID] = exs
	}
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	startMS, endMS = parseDownArgs(tfMSecs, startMS, endMS, limit, false)
	retErr := BulkDownOHLCV(exchange, exsMap, timeFrame, startMS, endMS, 0)
	if retErr != nil {
		return retErr
	}
	if handler == nil {
		return nil
	}
	itemNum := (endMS - startMS) / tfMSecs
	sess, conn, err := Conn(nil)
	if err != nil {
		return err
	}
	defer conn.Release()
	if itemNum < int64(core.KBatchSize) {
		sidArr := utils.KeysOfMap(exsMap)
		bulkHandler := func(sid int32, klines []*banexg.Kline) {
			exs, ok := exsMap[sid]
			if !ok {
				return
			}
			handler(exs.Symbol, timeFrame, klines)
		}
		return sess.QueryOHLCVBatch(sidArr, timeFrame, startMS, endMS, limit, bulkHandler)
	}
	// 单个数量过多，逐个查询
	for sid, exs := range exsMap {
		kline, err := sess.QueryOHLCV(sid, timeFrame, startMS, endMS, limit, false)
		if err != nil {
			return err
		}
		handler(exs.Symbol, timeFrame, kline)
	}
	return nil
}

func parseDownArgs(tfMSecs int64, startMS, endMS int64, limit int, withUnFinish bool) (int64, int64) {
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
