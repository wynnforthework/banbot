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
	utils2 "github.com/banbox/banexg/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"sort"
	"strconv"
	"strings"
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

func DownOHLCV2DB(exchange banexg.BanExchange, exs *ExSymbol, timeFrame string, startMS, endMS int64,
	pBar *utils.PrgBar) (int, *errs.Error) {
	sess, conn, err := Conn(nil)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	oldStart, oldEnd := sess.GetKlineRange(exs.ID, timeFrame)
	startMS = exs.GetValidStart(startMS)
	return downOHLCV2DBRange(sess, exchange, exs, timeFrame, startMS, endMS, oldStart, oldEnd, pBar)
}

/*
DownOHLCV2DB
下载K线到数据库，应在事务中调用此方法，否则查询更新相关数据会有错误
*/
func (q *Queries) DownOHLCV2DB(exchange banexg.BanExchange, exs *ExSymbol, timeFrame string, startMS, endMS int64,
	pBar *utils.PrgBar) (int, *errs.Error) {
	startMS = exs.GetValidStart(startMS)
	oldStart, oldEnd := q.GetKlineRange(exs.ID, timeFrame)
	return downOHLCV2DBRange(q, exchange, exs, timeFrame, startMS, endMS, oldStart, oldEnd, pBar)
}

/*
downOHLCV2DBRange
此函数会用于多线程下载，一个数据库会话只能用于一个线程，所以不能传入Queries
stepCB 用于更新进度，总值固定1000，避免内部下载区间大于传入区间
*/
func downOHLCV2DBRange(sess *Queries, exchange banexg.BanExchange, exs *ExSymbol, timeFrame string, startMS, endMS,
	oldStart, oldEnd int64, pBar *utils.PrgBar) (int, *errs.Error) {
	if oldStart <= startMS && endMS <= oldEnd || startMS <= exs.ListMs && endMS <= exs.ListMs || exs.Combined {
		// 完全处于已下载的区间 或 下载区间小于上市时间，无需下载
		if pBar != nil {
			pBar.Add(core.StepTotal)
		}
		return 0, nil
	}
	var err *errs.Error
	if sess == nil {
		var conn *pgxpool.Conn
		sess, conn, err = Conn(nil)
		if err != nil {
			if pBar != nil {
				pBar.Add(core.StepTotal)
			}
			return 0, err
		}
		defer conn.Release()
	}
	tfSecs := utils.TFToSecs(timeFrame)
	var totalNum int
	chanDown := make(chan *core.DownRange, 10)
	if oldStart == 0 {
		// 数据不存在，下载全部区间
		chanDown <- &core.DownRange{Start: startMS, End: endMS}
		totalNum = int((endMS-startMS)/1000) / tfSecs
	} else {
		if endMS > oldEnd {
			// 后部超过已下载范围，下载后面
			chanDown <- &core.DownRange{Start: oldEnd, End: endMS}
			totalNum += int((endMS-oldEnd)/1000) / tfSecs
		}
		if startMS < oldStart {
			// 前部超过已下载范围，下载前面
			chanDown <- &core.DownRange{Start: startMS, End: oldStart, Reverse: true}
			totalNum += int((oldStart-startMS)/1000) / tfSecs
		}
	}
	if pBar == nil && totalNum > 10000 {
		pBar = utils.NewPrgBar(core.StepTotal, exs.Symbol)
		defer pBar.Close()
	}
	var bar *utils.PrgBarJob
	if pBar != nil {
		bar = pBar.NewJob(totalNum)
		defer bar.Done()
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
				err = FetchApiOHLCV(ctx, exchange, exs.Symbol, timeFrame, start, stop, chanKline)
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
					if bar != nil {
						bar.Add(curNum)
					}
					saveNum += curNum
				}
			}
		}
	}()

	wg.Wait()
	if outErr != nil {
		return saveNum, errs.New(core.ErrRunTime, outErr)
	}
	err = sess.UpdateKRange(exs.ID, timeFrame, startMS, endMS, nil, true)
	return saveNum, err
}

/*
AutoFetchOHLCV

	获取给定交易对，给定时间维度，给定范围的K线数据。
	先尝试从本地读取，不存在时从交易所下载，然后返回。
*/
func AutoFetchOHLCV(exchange banexg.BanExchange, exs *ExSymbol, timeFrame string, startMS, endMS int64,
	limit int, withUnFinish bool, pBar *utils.PrgBar) ([]*banexg.Kline, *errs.Error) {
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	startMS, endMS = parseDownArgs(tfMSecs, startMS, endMS, limit, withUnFinish)
	downTF, err := GetDownTF(timeFrame)
	if err != nil {
		if pBar != nil {
			pBar.Add(core.StepTotal)
		}
		return nil, err
	}
	sess, conn, err := Conn(nil)
	if err != nil {
		if pBar != nil {
			pBar.Add(core.StepTotal)
		}
		return nil, err
	}
	defer conn.Release()
	_, err = sess.DownOHLCV2DB(exchange, exs, downTF, startMS, endMS, pBar)
	if err != nil {
		// DownOHLCV2DB 内部已处理stepCB，这里无需处理
		return nil, err
	}
	return sess.GetOHLCV(exs, timeFrame, startMS, endMS, limit, withUnFinish)
}

/*
GetOHLCV 获取品种K线，如需复权自动前复权
*/
func GetOHLCV(exs *ExSymbol, timeFrame string, startMS, endMS int64, limit int, withUnFinish bool) ([]*banexg.Kline, *errs.Error) {
	sess, conn, err := Conn(nil)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	return sess.GetOHLCV(exs, timeFrame, startMS, endMS, limit, withUnFinish)
}

/*
GetOHLCV 获取品种K线，如需复权自动前复权
*/
func (q *Queries) GetOHLCV(exs *ExSymbol, timeFrame string, startMS, endMS int64, limit int, withUnFinish bool) ([]*banexg.Kline, *errs.Error) {
	if exs.Exchange == "china" && exs.Market != banexg.MarketSpot {
		// 国内非股票，可能是：期货、期权、基金、、、
		parts := utils2.SplitParts(exs.Symbol)
		if len(parts) >= 2 {
			p2val := parts[1].Val
			if p2val == "888" {
				// 期货888是主力连续合约，000是指数合约
				return q.GetFacOHLCV(exs.ID, timeFrame, startMS, endMS, limit, withUnFinish)
			}
		}
	}
	return q.QueryOHLCV(exs.ID, timeFrame, startMS, endMS, limit, withUnFinish)
}

/*
GetFacOHLCV 获取前复权的K线
*/
func (q *Queries) GetFacOHLCV(sid int32, timeFrame string, startMS, endMS int64, limit int, withUnFinish bool) ([]*banexg.Kline, *errs.Error) {
	ctx := context.Background()
	facs, err_ := q.GetAdjFactors(ctx, sid)
	if err_ != nil {
		return nil, errs.New(core.ErrDbReadFail, err_)
	}
	// facs已按时间倒序排序
	// price(i) * factor(i-1) * factor(i-2) * ... factor(1) = latest price
	lastFac := float64(1)
	rid := len(facs) - 1
	for i, f := range facs {
		f.Factor *= lastFac
		lastFac = f.Factor
		if f.StartMs < startMS {
			rid = i
			break
		}
	}
	facs = facs[:rid+1]
	var result []*banexg.Kline
	// 从后往前查询K线，并进行前复权
	curEnd := endMS
	factor := float64(1)
	for _, f := range facs {
		if f.StartMs >= endMS {
			factor = f.Factor
			continue
		}
		curSid := f.SubID
		if curSid == 0 {
			curSid = sid
		}
		start := max(f.StartMs, startMS)
		klines, err := q.QueryOHLCV(curSid, timeFrame, start, curEnd, limit, withUnFinish)
		if err != nil {
			return nil, err
		}
		if factor != 1 {
			for _, k := range klines {
				k.Open *= factor
				k.High *= factor
				k.Low *= factor
				k.Close *= factor
			}
		}
		result = append(klines, result...)
		curEnd = f.StartMs
		withUnFinish = false
		if limit > 0 && len(result) >= limit {
			if len(result) > limit {
				result = result[len(result)-limit:]
			}
			break
		}
		factor = f.Factor
	}
	return result, nil
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
	barNum := int((endMS - startMS) / tfMSecs)
	startText := btime.ToDateStr(startMS, "")
	endText := btime.ToDateStr(endMS, "")
	log.Info(fmt.Sprintf("bulk down %s %d pairs %s-%s, len:%d\n", timeFrame, len(exsList), startText, endText, barNum))
	var pBar = utils.NewPrgBar(len(exsList)*core.StepTotal, "")
	defer pBar.Close()
	sess, conn, err := Conn(nil)
	if err != nil {
		return err
	}
	sidList := utils.KeysOfMap(exsList)
	// 这里应该使用更小的downTF
	kRanges := sess.GetKlineRanges(sidList, downTF)
	conn.Release()
	return utils.ParallelRun(sidList, core.ConcurNum, func(i int32) *errs.Error {
		exs, _ := exsList[i]
		var oldStart, oldEnd = int64(0), int64(0)
		if krange, ok := kRanges[exs.ID]; ok {
			oldStart, oldEnd = krange[0], krange[1]
		}
		_, err = downOHLCV2DBRange(nil, exchange, exs, downTF, startMS, endMS, oldStart, oldEnd, pBar)
		return err
	})
}

/*
FastBulkOHLCV
快速批量获取K线。先下载所有需要的币种，然后批量查询再分组返回。

	适用于币种较多，且需要的开始结束时间一致，且大部分已下载的情况。
*/
func FastBulkOHLCV(exchange banexg.BanExchange, symbols []string, timeFrame string,
	startMS, endMS int64, limit int, handler func(string, string, []*banexg.Kline)) *errs.Error {
	var exsMap, err = MapExSymbols(exchange, symbols)
	if err != nil {
		return err
	}
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	startMS, endMS = parseDownArgs(tfMSecs, startMS, endMS, limit, false)
	exInfo := exchange.Info()
	if exchange.HasApi(banexg.ApiFetchOHLCV, exInfo.MarketType) {
		retErr := BulkDownOHLCV(exchange, exsMap, timeFrame, startMS, endMS, 0)
		if retErr != nil {
			return retErr
		}
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
	leftArr := make([]int32, 0, len(exsMap))
	if itemNum < int64(core.KBatchSize) {
		sidArr := make([]int32, 0, len(exsMap))
		for sid, exs := range exsMap {
			if exs.Combined {
				leftArr = append(leftArr, sid)
			} else {
				sidArr = append(sidArr, sid)
			}
		}
		if len(sidArr) > 0 {
			bulkHandler := func(sid int32, klines []*banexg.Kline) {
				exs, ok := exsMap[sid]
				if !ok {
					return
				}
				handler(exs.Symbol, timeFrame, klines)
			}
			err = sess.QueryOHLCVBatch(sidArr, timeFrame, startMS, endMS, limit, bulkHandler)
			if err != nil {
				return err
			}
		}
	} else {
		leftArr = utils.KeysOfMap(exsMap)
	}
	// 单个数量过多，逐个查询
	for _, sid := range leftArr {
		exs := exsMap[sid]
		kline, err := sess.GetOHLCV(exs, timeFrame, startMS, endMS, limit, false)
		if err != nil {
			return err
		}
		handler(exs.Symbol, timeFrame, kline)
	}
	return nil
}

func MapExSymbols(exchange banexg.BanExchange, symbols []string) (map[int32]*ExSymbol, *errs.Error) {
	var exsMap = make(map[int32]*ExSymbol)
	for _, pair := range symbols {
		exs, err := GetExSymbol(exchange, pair)
		if err != nil {
			return exsMap, err
		}
		exsMap[exs.ID] = exs
	}
	return exsMap, nil
}

func parseDownArgs(tfMSecs int64, startMS, endMS int64, limit int, withUnFinish bool) (int64, int64) {
	if startMS > 0 && startMS != core.MSMinStamp {
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
	if startMS == 0 && limit > 0 {
		startMS = endMS - tfMSecs*int64(limit)
	}
	return startMS, endMS
}

func (q *Queries) getCalendars(name string, startMS, stopMS int64, fields string) (pgx.Rows, error) {
	var b strings.Builder
	b.WriteString("select ")
	b.WriteString(fields)
	b.WriteString(" from calendars where name=$1 ")
	if startMS > 0 {
		b.WriteString(fmt.Sprintf("and stop_ms > %v ", startMS))
	}
	if stopMS > 0 {
		b.WriteString(fmt.Sprintf("and start_ms < %v ", stopMS))
	}
	b.WriteString("order by start_ms")
	ctx := context.Background()
	return q.db.Query(ctx, b.String(), name)
}

func (q *Queries) GetCalendars(name string, startMS, stopMS int64) ([][2]int64, *errs.Error) {
	rows, err_ := q.getCalendars(name, startMS, stopMS, "start_ms,stop_ms")
	if err_ != nil {
		return nil, errs.New(core.ErrDbReadFail, err_)
	}
	defer rows.Close()
	result := make([][2]int64, 0)
	for rows.Next() {
		var start, stop int64
		err_ = rows.Scan(&start, &stop)
		if err_ != nil {
			return result, errs.New(core.ErrDbReadFail, err_)
		}
		result = append(result, [2]int64{start, stop})
	}
	return result, nil
}

func (q *Queries) SetCalendars(name string, items [][2]int64) *errs.Error {
	if len(items) == 0 {
		return nil
	}
	startMS, stopMS := items[0][0], items[len(items)-1][1]
	rows, err_ := q.getCalendars(name, startMS, stopMS, "id,start_ms,stop_ms")
	if err_ != nil {
		return errs.New(core.ErrDbReadFail, err_)
	}
	defer rows.Close()
	olds := make([]*Calendar, 0)
	for rows.Next() {
		var cal = &Calendar{}
		err_ = rows.Scan(&cal.ID, &cal.StartMs, &cal.StopMs)
		if err_ != nil {
			return errs.New(core.ErrDbReadFail, err_)
		}
		olds = append(olds, cal)
	}
	ctx := context.Background()
	if len(olds) > 0 {
		items[0][0] = olds[0].StartMs
		items[len(items)-1][1] = olds[len(olds)-1].StopMs
		ids := make([]string, len(olds))
		for i, o := range olds {
			ids[i] = strconv.Itoa(int(o.ID))
		}
		sql := fmt.Sprintf("delete from calendars where id in (%s)", strings.Join(ids, ","))
		_, err_ = q.db.Exec(ctx, sql)
		if err_ != nil {
			return errs.New(core.ErrDbExecFail, err_)
		}
	}
	adds := make([]AddCalendarsParams, 0, len(items))
	for _, tu := range items {
		adds = append(adds, AddCalendarsParams{Name: name, StartMs: tu[0], StopMs: tu[1]})
	}
	_, err_ = q.AddCalendars(ctx, adds)
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	return nil
}

/*
GetExSHoles
获取指定Sid在某个时间段内，所有非交易时间范围。
对于币圈365*24不休，返回空
*/
func (q *Queries) GetExSHoles(exchange banexg.BanExchange, exs *ExSymbol, start, stop int64, full bool) ([][2]int64, *errs.Error) {
	exInfo := exchange.Info()
	if exInfo.FullDay && exInfo.NoHoliday {
		// 365天全年无休，且24小时可交易，不存在休息时间段
		return nil, nil
	}
	mar, err := exchange.GetMarket(exs.Symbol)
	if err != nil {
		return nil, err
	}
	var dtList [][2]int64
	if full {
		// 不使用交易日过滤
		dayMSecs := int64(utils.TFToSecs("1d") * 1000)
		curTime := utils.AlignTfMSecs(start, dayMSecs)
		for curTime < stop {
			curEnd := curTime + dayMSecs
			dtList = append(dtList, [2]int64{curTime, curEnd})
			curTime = curEnd
		}
	} else {
		// 获取交易日
		dtList, err = q.GetCalendars(mar.ExgReal, start, stop)
		if err != nil {
			return nil, err
		}
		if len(dtList) == 0 {
			// 给定时间段没有可交易日。整个作为hole
			return [][2]int64{{start, stop}}, nil
		}
	}
	times := make([][2]int64, 0, len(mar.DayTimes))
	times = append(times, mar.DayTimes...)
	times = append(times, mar.NightTimes...)
	sort.Slice(times, func(i, j int) bool {
		return times[i][0] < times[j][0]
	})
	if len(times) == 0 {
		if !exInfo.FullDay {
			log.Warn("day_ranges/night_ranges invalid", zap.String("id", mar.ID))
		}
		times = [][2]int64{{0, 24 * 60 * 60000}}
	}
	res := make([][2]int64, 0)
	lastStop := int64(0)
	if times[0][0] > 0 {
		lastStop = dtList[0][0]
	}
	for _, dt := range dtList {
		for _, rg := range times {
			if lastStop > 0 {
				res = append(res, [2]int64{lastStop, dt[0] + rg[0]})
			}
			lastStop = dt[0] + rg[1]
		}
	}
	validStop := dtList[len(dtList)-1][0] + times[len(times)-1][1]
	if validStop < stop {
		res = append(res, [2]int64{validStop, stop})
	}
	return res, nil
}
