package orm

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var (
	aggList = []*KlineAgg{
		//全部使用超表，自行在插入时更新依赖表。因连续聚合无法按sid刷新，在按sid批量插入历史数据后刷新时性能较差
		NewKlineAgg("1m", "kline_1m", "", "", "", "", "2 months", "12 months"),
		NewKlineAgg("5m", "kline_5m", "1m", "20m", "1m", "1m", "2 months", "12 months"),
		NewKlineAgg("15m", "kline_15m", "5m", "1h", "5m", "5m", "3 months", "16 months"),
		NewKlineAgg("1h", "kline_1h", "", "", "", "", "6 months", "3 years"),
		NewKlineAgg("1d", "kline_1d", "1h", "3d", "1h", "1h", "3 years", "20 years"),
	}
	aggMap  = make(map[string]*KlineAgg)
	downTfs = map[string]struct{}{"1m": {}, "15m": {}, "1h": {}, "1d": {}}
)

const (
	aggFields = `
  first(open, time) AS open,  
  max(high) AS high,
  min(low) AS low, 
  last(close, time) AS close,
  sum(volume) AS volume`
	klineInsConflict = `
ON CONFLICT (sid, time)
DO UPDATE SET 
open = EXCLUDED.open,
high = EXCLUDED.high,
low = EXCLUDED.low,
close = EXCLUDED.close,
volume = EXCLUDED.volume`
)

func init() {
	for _, agg := range aggList {
		aggMap[agg.TimeFrame] = agg
	}
}

func (q *Queries) QueryOHLCV(sid int32, timeframe string, startMs, endMs int64, limit int, withUnFinish bool) ([]*banexg.Kline, *errs.Error) {
	tfMSecs := int64(utils.TFToSecs(timeframe) * 1000)
	revRead := startMs == 0 && limit > 0
	startMs, endMs = parseDownArgs(tfMSecs, startMs, endMs, limit, withUnFinish)
	maxEndMs := endMs
	finishEndMS := utils.AlignTfMSecs(endMs, tfMSecs)
	unFinishMS := int64(0)
	if core.LiveMode && withUnFinish {
		curMs := btime.TimeMS()
		unFinishMS = utils.AlignTfMSecs(curMs, tfMSecs)
		if finishEndMS > unFinishMS {
			finishEndMS = unFinishMS
		}
	}
	var dctSql string
	if revRead {
		// 未提供开始时间，提供了数量限制，按时间倒序搜索
		dctSql = fmt.Sprintf(`
select time,open,high,low,close,volume from $tbl
where sid=%d and time < %v
order by time desc limit %v`, sid, finishEndMS, limit)
	} else {
		if limit == 0 {
			limit = int((finishEndMS-startMs)/tfMSecs) + 1
		}
		dctSql = fmt.Sprintf(`
select time,open,high,low,close,volume from $tbl
where sid=%d and time >= %v and time < %v
order by time limit %v`, sid, startMs, finishEndMS, limit)
	}
	subTF, rows, err_ := queryHyper(q, timeframe, dctSql)
	klines, err_ := mapToKlines(rows, err_)
	if err_ != nil {
		return nil, errs.New(core.ErrDbReadFail, err_)
	}
	if revRead {
		// 倒序读取的，再次逆序，使时间升序
		utils.ReverseArr(klines)
	}
	if subTF != "" && len(klines) > 0 {
		fromTfMSecs := int64(utils.TFToSecs(subTF) * 1000)
		var lastFinish bool
		klines, lastFinish = utils.BuildOHLCV(klines, tfMSecs, 0, nil, fromTfMSecs)
		if !lastFinish && len(klines) > 0 {
			klines = klines[:len(klines)-1]
		}
	}
	if len(klines) == 0 && maxEndMs-endMs > tfMSecs {
		return q.QueryOHLCV(sid, timeframe, endMs, maxEndMs, limit, withUnFinish)
	} else if withUnFinish && len(klines) > 0 && klines[len(klines)-1].Time+tfMSecs == unFinishMS {
		unbar, _, _ := getUnFinish(q, sid, timeframe, unFinishMS, unFinishMS+tfMSecs, "query")
		if unbar != nil {
			klines = append(klines, unbar)
		}
	}
	return klines, nil
}

type KlineSid struct {
	banexg.Kline
	Sid int32
}

func (q *Queries) QueryOHLCVBatch(sids []int32, timeframe string, startMs, endMs int64, limit int, handle func(int32, []*banexg.Kline)) *errs.Error {
	if len(sids) == 0 {
		return nil
	}
	tfMSecs := int64(utils.TFToSecs(timeframe) * 1000)
	startMs, endMs = parseDownArgs(tfMSecs, startMs, endMs, limit, false)
	finishEndMS := utils.AlignTfMSecs(endMs, tfMSecs)
	if core.LiveMode {
		curMs := btime.TimeMS()
		unFinishMS := utils.AlignTfMSecs(curMs, tfMSecs)
		if finishEndMS > unFinishMS {
			finishEndMS = unFinishMS
		}
	}
	sidTA := make([]string, len(sids))
	for i, id := range sids {
		sidTA[i] = fmt.Sprintf("%v", id)
	}
	sidText := strings.Join(sidTA, ", ")
	dctSql := fmt.Sprintf(`
select time,open,high,low,close,volume,sid from $tbl
where time >= %v and time < %v and sid in (%v)
order by sid,time`, startMs, finishEndMS, sidText)
	subTF, rows, err_ := queryHyper(q, timeframe, dctSql)
	arrs, err_ := mapToItems(rows, err_, func() (*KlineSid, []any) {
		var i KlineSid
		return &i, []any{&i.Time, &i.Open, &i.High, &i.Low, &i.Close, &i.Volume, &i.Sid}
	})
	if err_ != nil {
		return errs.New(core.ErrDbReadFail, err_)
	}
	initCap := max(len(arrs)/len(sids), 16)
	var klineArr []*banexg.Kline
	curSid := int32(0)
	fromTfMSecs := int64(0)
	if subTF != "" {
		fromTfMSecs = int64(utils.TFToSecs(subTF) * 1000)
	}
	noFired := make(map[int32]bool)
	for _, sid := range sids {
		noFired[sid] = true
	}
	callBack := func() {
		if fromTfMSecs > 0 {
			var lastDone bool
			klineArr, lastDone = utils.BuildOHLCV(klineArr, tfMSecs, 0, nil, fromTfMSecs)
			if !lastDone {
				klineArr = klineArr[:len(klineArr)-1]
			}
		}
		if len(klineArr) > 0 {
			delete(noFired, curSid)
			handle(curSid, klineArr)
		}
	}
	// callBack for kline pairs
	for _, k := range arrs {
		if k.Sid != curSid {
			if curSid > 0 && len(klineArr) > 0 {
				callBack()
			}
			curSid = k.Sid
			klineArr = make([]*banexg.Kline, 0, initCap)
		}
		klineArr = append(klineArr, &banexg.Kline{Time: k.Time, Open: k.Open, High: k.High, Low: k.Low,
			Close: k.Close, Volume: k.Volume})
	}
	if curSid > 0 && len(klineArr) > 0 {
		callBack()
	}
	// callBack for no data sids
	for sid := range noFired {
		handle(sid, nil)
	}
	return nil
}

func (q *Queries) getKLineTimes(sid int32, timeframe string, startMs, endMs int64) ([]int64, *errs.Error) {
	tblName := "kline_" + timeframe
	dctSql := fmt.Sprintf(`
select time from %s
where sid=%d and time >= %v and time < %v
order by time`, tblName, sid, startMs, endMs)
	rows, err_ := q.db.Query(context.Background(), dctSql)
	res, err_ := mapToItems(rows, err_, func() (*int64, []any) {
		var t int64
		return &t, []any{&t}
	})
	if err_ != nil {
		return nil, errs.New(core.ErrDbReadFail, err_)
	}
	resList := make([]int64, len(res))
	for i, v := range res {
		resList[i] = *v
	}
	return resList, nil
}

func queryHyper(sess *Queries, timeFrame, sql string, args ...interface{}) (string, pgx.Rows, error) {
	agg, ok := aggMap[timeFrame]
	var subTF, table string
	if ok {
		table = agg.Table
	} else {
		// 时间帧没有直接符合的，从最接近的子timeframe聚合
		subTF, table = getSubTf(timeFrame)
	}
	sql = strings.Replace(sql, "$tbl", table, 1)
	rows, err := sess.db.Query(context.Background(), sql, args...)
	return subTF, rows, err
}

func mapToKlines(rows pgx.Rows, err_ error) ([]*banexg.Kline, error) {
	return mapToItems(rows, err_, func() (*banexg.Kline, []any) {
		var i banexg.Kline
		return &i, []any{&i.Time, &i.Open, &i.High, &i.Low, &i.Close, &i.Volume}
	})
}

func getSubTf(timeFrame string) (string, string) {
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	for i := len(aggList) - 1; i >= 0; i-- {
		agg := aggList[i]
		if agg.MSecs >= tfMSecs {
			continue
		}
		if tfMSecs%agg.MSecs == 0 {
			return agg.TimeFrame, agg.Table
		}
	}
	return "", ""
}

func (q *Queries) PurgeKlineUn() *errs.Error {
	sql := "delete from kline_un"
	return q.Exec(sql)
}

/*
getUnFinish
查询给定周期的未完成bar。给定周期可以是保存的周期1m,5m,15m,1h,1d；也可以是聚合周期如4h,3d
此方法两种用途：query用户查询最新数据（可能是聚合周期）；calc从子周期更新大周期的未完成bar（不可能是聚合周期）
返回的错误表示数据不存在
*/
func getUnFinish(sess *Queries, sid int32, timeFrame string, startMS, endMS int64, mode string) (*banexg.Kline, int64, error) {
	if mode != "calc" && mode != "query" {
		panic(fmt.Sprintf("`mode` of getUnFinish must be calc/query, current: %s", mode))
	}
	ctx := context.Background()
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	barEndMS := int64(0)
	fromTF := timeFrame
	var bigKlines = make([]*banexg.Kline, 0)
	if _, ok := aggMap[timeFrame]; mode == "calc" || !ok {
		// 从已完成的子周期中归集数据
		fromTF, _ = getSubTf(timeFrame)
		aggFrom := "kline_" + fromTF
		sql := fmt.Sprintf(`select time,open,high,low,close,volume from %s
where sid=%d and time >= %v and time < %v`, aggFrom, sid, startMS, endMS)
		rows, err_ := sess.db.Query(ctx, sql)
		klines, err_ := mapToKlines(rows, err_)
		if err_ != nil {
			return nil, 0, err_
		}
		bigKlines, _ = utils.BuildOHLCV(klines, tfMSecs, 0, nil, 0)
		if len(klines) > 0 {
			barEndMS = klines[len(klines)-1].Time + int64(utils.TFToSecs(fromTF)*1000)
		}
	}
	// 从未完成的周期/子周期中查询数据
	sql := fmt.Sprintf(`SELECT start_ms,open,high,low,close,volume,stop_ms FROM kline_un
						where sid=%d and timeframe='%s' and start_ms >= %d
						limit 1`, sid, fromTF, startMS)
	row := sess.db.QueryRow(ctx, sql)
	var unbar = &banexg.Kline{}
	var unToMS = int64(0)
	err_ := row.Scan(&unbar.Time, &unbar.Open, &unbar.High, &unbar.Low, &unbar.Close, &unbar.Volume, &unToMS)
	if err_ != nil {
		return nil, 0, err_
	} else if unbar.Volume > 0 {
		bigKlines = append(bigKlines, unbar)
		barEndMS = max(barEndMS, unToMS)
	}
	if len(bigKlines) == 0 {
		return nil, barEndMS, nil
	}
	if len(bigKlines) == 1 {
		return bigKlines[0], barEndMS, nil
	} else {
		var res = bigKlines[0]
		for _, bar := range bigKlines[1:] {
			res.High = max(res.High, bar.High)
			res.Low = min(res.Low, bar.Low)
			res.Volume += bar.Volume
		}
		res.Close = bigKlines[len(bigKlines)-1].Close
		return res, barEndMS, nil
	}
}

/*
calcUnFinish
从子周期计算大周期的未完成bar
*/
func calcUnFinish(sid int32, timeFrame, subTF string, startMS, endMS int64, arr []*banexg.Kline) *KlineUn {
	toTfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	fromTfMSecs := int64(utils.TFToSecs(subTF) * 1000)
	merged, _ := utils.BuildOHLCV(arr, toTfMSecs, 0, nil, fromTfMSecs)
	out := merged[len(merged)-1]
	return &KlineUn{
		Sid:       sid,
		StartMs:   startMS,
		Open:      out.Open,
		High:      out.High,
		Low:       out.Low,
		Close:     out.Close,
		Volume:    out.Volume,
		StopMs:    endMS,
		Timeframe: timeFrame,
	}
}

func updateUnFinish(sess *Queries, agg *KlineAgg, sid int32, subTF string, startMS, endMS int64, klines []*banexg.Kline) *errs.Error {
	tfMSecs := int64(utils.TFToSecs(agg.TimeFrame) * 1000)
	finished := endMS%tfMSecs == 0
	whereSql := fmt.Sprintf("where sid=%v and timeframe='%v';", sid, agg.TimeFrame)
	fromWhere := "from kline_un " + whereSql
	ctx := context.Background()
	if finished {
		_, err_ := sess.db.Exec(ctx, "delete "+fromWhere)
		if err_ != nil {
			return errs.New(core.ErrDbExecFail, err_)
		}
		return nil
	}
	if len(klines) == 0 {
		log.Warn("skip unFinish for empty", zap.Int64("s", startMS), zap.Int64("e", endMS))
		return nil
	}
	sub := calcUnFinish(sid, agg.TimeFrame, subTF, startMS, endMS, klines)
	barStartMS := utils.AlignTfMSecs(startMS, tfMSecs)
	barEndMS := utils.AlignTfMSecs(endMS, tfMSecs)
	if barStartMS == barEndMS {
		// 当子周期插入开始结束时间戳，对应到当前周期，属于同一个bar时，才执行快速更新
		sql := "select start_ms,open,high,low,close,volume,stop_ms " + fromWhere
		row := sess.db.QueryRow(ctx, sql)
		var unBar KlineUn
		err_ := row.Scan(&unBar.StartMs, &unBar.Open, &unBar.High, &unBar.Low, &unBar.Close, &unBar.Volume, &unBar.StopMs)
		if err_ == nil && unBar.StopMs == startMS {
			//当本次插入开始时间戳，和未完成bar结束时间戳完全匹配时，认为有效
			unBar.High = max(unBar.High, sub.High)
			unBar.Low = min(unBar.Low, sub.Low)
			unBar.Close = sub.Close
			unBar.Volume += sub.Volume
			unBar.StopMs = endMS
			updSql := fmt.Sprintf("update kline_un set high=%v,low=%v,close=%v,volume=%v,stop_ms=%v %s",
				unBar.High, unBar.Low, unBar.Close, unBar.Volume, unBar.StopMs, whereSql)
			_, err_ = sess.db.Exec(ctx, updSql)
			if err_ != nil {
				return errs.New(core.ErrDbExecFail, err_)
			}
			return nil
		}
	}
	//当快速更新不可用时，从子周期归集
	_, err_ := sess.db.Exec(ctx, "delete "+fromWhere)
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	return sess.Exec(`insert into kline_un (sid, start_ms, stop_ms, open, high, low, close, volume, timeframe) 
values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, sub.Sid, sub.StartMs, endMS, sub.Open, sub.High, sub.Low,
		sub.Close, sub.Volume, sub.Timeframe)
}

// iterForAddKLines implements pgx.CopyFromSource.
type iterForAddKLines struct {
	rows                 []*KlineSid
	skippedFirstNextCall bool
}

func (r *iterForAddKLines) Next() bool {
	if len(r.rows) == 0 {
		return false
	}
	if !r.skippedFirstNextCall {
		r.skippedFirstNextCall = true
		return true
	}
	r.rows = r.rows[1:]
	return len(r.rows) > 0
}

func (r iterForAddKLines) Values() ([]interface{}, error) {
	return []interface{}{
		r.rows[0].Sid,
		r.rows[0].Time,
		r.rows[0].Open,
		r.rows[0].High,
		r.rows[0].Low,
		r.rows[0].Close,
		r.rows[0].Volume,
	}, nil
}

func (r iterForAddKLines) Err() error {
	return nil
}

/*
InsertKLines
只批量插入K线，如需同时更新关联信息，请使用InsertKLinesAuto
*/
func (q *Queries) InsertKLines(timeFrame string, sid int32, arr []*banexg.Kline) (int64, *errs.Error) {
	arrLen := len(arr)
	if arrLen == 0 {
		return 0, nil
	}
	log.Debug("insert klines", zap.String("tf", timeFrame), zap.Int32("sid", sid),
		zap.Int("num", arrLen), zap.Int64("start", arr[0].Time), zap.Int64("end", arr[arrLen-1].Time))
	tblName := "kline_" + timeFrame
	var adds = make([]*KlineSid, arrLen)
	for i, v := range arr {
		adds[i] = &KlineSid{
			Kline: *v,
			Sid:   sid,
		}
	}
	ctx := context.Background()
	cols := []string{"sid", "time", "open", "high", "low", "close", "volume"}
	num, err_ := q.db.CopyFrom(ctx, []string{tblName}, cols, &iterForAddKLines{rows: adds})
	if err_ != nil {
		return 0, errs.New(core.ErrDbExecFail, err_)
	}
	return num, nil
}

/*
InsertKLinesAuto
插入K线到数据库，同时调用UpdateKRange更新关联信息
调用此方法前必须通过GetKlineRange自行判断数据库中是否已存在，避免重复插入
*/
func (q *Queries) InsertKLinesAuto(timeFrame string, sid int32, arr []*banexg.Kline, aggBig bool) (int64, *errs.Error) {
	num, err := q.InsertKLines(timeFrame, sid, arr)
	if err != nil {
		return num, err
	}
	startMS := arr[0].Time
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	endMS := arr[len(arr)-1].Time + tfMSecs
	err = q.UpdateKRange(sid, timeFrame, startMS, endMS, arr, aggBig)
	return num, err
}

/*
UpdateKRange
1. 更新K线的有效区间
2. 搜索空洞，更新Khole
3. 更新更大周期的连续聚合
*/
func (q *Queries) UpdateKRange(sid int32, timeFrame string, startMS, endMS int64, klines []*banexg.Kline, aggBig bool) *errs.Error {
	// 更新有效区间范围
	err := q.updateKLineRange(sid, timeFrame, startMS, endMS)
	if err != nil {
		return err
	}
	// 搜索空洞，更新khole
	err = q.updateKHoles(sid, timeFrame, startMS, endMS)
	if err != nil {
		return err
	}
	if !aggBig {
		return nil
	}
	// 更新更大的超表
	return q.updateBigHyper(sid, timeFrame, startMS, endMS, klines)
}

/*
CalcKLineRange
计算指定周期K线在指定范围内，有效区间。
*/
func (q *Queries) CalcKLineRange(sid int32, timeFrame string, start, end int64) (int64, int64, *errs.Error) {
	tblName := "kline_" + timeFrame
	sql := fmt.Sprintf("select min(time),max(time) from %s where sid=%v", tblName, sid)
	if start > 0 {
		sql += fmt.Sprintf(" and time>=%v", start)
	}
	if end > 0 {
		sql += fmt.Sprintf(" and time<%v", end)
	}
	ctx := context.Background()
	row := q.db.QueryRow(ctx, sql)
	var realStart, realEnd *int64
	err_ := row.Scan(&realStart, &realEnd)
	if err_ != nil {
		return 0, 0, errs.New(core.ErrDbReadFail, err_)
	}
	if realEnd == nil {
		realStart = new(int64)
		realEnd = new(int64)
	}
	if *realEnd > 0 {
		// 修正为实际结束时间
		*realEnd += int64(utils.TFToSecs(timeFrame) * 1000)
	}
	return *realStart, *realEnd, nil
}

func (q *Queries) CalcKLineRanges(timeFrame string) (map[int32][2]int64, *errs.Error) {
	tblName := "kline_" + timeFrame
	sql := fmt.Sprintf("select sid,min(time),max(time) from %s group by sid", tblName)
	ctx := context.Background()
	rows, err_ := q.db.Query(ctx, sql)
	if err_ != nil {
		return nil, errs.New(core.ErrDbReadFail, err_)
	}
	defer rows.Close()
	res := make(map[int32][2]int64)
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	for rows.Next() {
		var sid int32
		var realStart, realEnd int64
		err_ = rows.Scan(&sid, &realStart, &realEnd)
		res[sid] = [2]int64{realStart, realEnd + tfMSecs}
		if err_ != nil {
			return res, errs.New(core.ErrDbReadFail, err_)
		}
	}
	err_ = rows.Err()
	if err_ != nil {
		return res, errs.New(core.ErrDbReadFail, err_)
	}
	return res, nil
}

func (q *Queries) updateKLineRange(sid int32, timeFrame string, startMS, endMS int64) *errs.Error {
	// 更新有效区间范围
	var err_ error
	ctx := context.Background()
	realStart, realEnd, err := q.CalcKLineRange(sid, timeFrame, 0, 0)
	if err != nil {
		_, err_ = q.AddKInfo(ctx, AddKInfoParams{Sid: sid, Timeframe: timeFrame, Start: startMS, Stop: endMS})
		if err_ != nil {
			return errs.New(core.ErrDbExecFail, err_)
		}
		log.Debug("add kinfo", zap.Int32("sid", sid), zap.String("tf", timeFrame),
			zap.Int64("start", startMS), zap.Int64("end", endMS))
		return nil
	}
	if realStart == 0 || realEnd == 0 {
		realStart, realEnd = startMS, endMS
	} else {
		realStart = min(realStart, startMS)
		realEnd = max(realEnd, endMS)
	}
	oldStart, _ := q.GetKlineRange(sid, timeFrame)
	if oldStart == 0 {
		_, err_ = q.AddKInfo(ctx, AddKInfoParams{Sid: sid, Timeframe: timeFrame, Start: realStart, Stop: realEnd})
	} else {
		err_ = q.SetKInfo(ctx, SetKInfoParams{Sid: sid, Timeframe: timeFrame, Start: realStart, Stop: realEnd})
	}
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	log.Debug("set kinfo", zap.Int32("sid", sid), zap.String("tf", timeFrame),
		zap.Int64("start", startMS), zap.Int64("end", endMS))
	return nil
}

func (q *Queries) updateKHoles(sid int32, timeFrame string, startMS, endMS int64) *errs.Error {
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	barTimes, err := q.getKLineTimes(sid, timeFrame, startMS, endMS)
	if err != nil {
		return err
	}
	// 从所有bar时间中找到缺失的kholes
	holes := make([][2]int64, 0)
	if len(barTimes) == 0 {
		holes = append(holes, [2]int64{startMS, endMS})
	} else {
		if barTimes[0] > startMS {
			holes = append(holes, [2]int64{startMS, barTimes[0]})
		}
		prevTime := barTimes[0]
		for _, time := range barTimes[1:] {
			intv := time - prevTime
			if intv > tfMSecs {
				holes = append(holes, [2]int64{prevTime + tfMSecs, time})
			} else if intv < tfMSecs {
				log.Error("invalid timeframe or kline", zap.Int32("sid", sid), zap.String("tf", timeFrame),
					zap.Int64("intv", intv), zap.Int64("tfmsecs", tfMSecs))
			}
			prevTime = time
		}
		maxEnd := utils.AlignTfMSecs(btime.UTCStamp(), tfMSecs) - tfMSecs
		if maxEnd-prevTime > tfMSecs*5 && endMS-prevTime > tfMSecs {
			holes = append(holes, [2]int64{prevTime + tfMSecs, min(endMS, maxEnd)})
		}
	}
	if len(holes) == 0 {
		return nil
	}
	// 检查法定休息时间段，过滤非交易时间段
	exs := GetSymbolByID(sid)
	exchange, err := exg.GetWith(exs.Exchange, exs.Market, "")
	if err != nil {
		return err
	}
	// 由于历史数据中部分交易日未录入，故不适用交易日历过滤K线
	susp, err := q.GetExSHoles(exchange, exs, startMS, endMS, true)
	if err != nil {
		return err
	}
	if len(susp) > 0 {
		// 过滤掉非交易时间段
		hs := make([][2]int64, 0, len(holes))
		si, hi := 0, -1
		for hi+1 < len(holes) {
			hi += 1
			h := holes[hi]
			for si < len(susp) && susp[si][1] <= h[0] {
				si += 1
			}
			if si >= len(susp) {
				hs = append(hs, holes[hi:]...)
				break
			}
			s := susp[si]
			if s[0] > h[0] {
				// 空洞左侧是有效区间
				hs = append(hs, [2]int64{h[0], min(h[1], s[0])})
			}
			if h[1] > s[1] {
				// 右侧有可能溢出
				holes[hi][0] = s[1]
				hi -= 1
			}
		}
		holes = hs
	}
	// 过滤太小的空洞
	if tfMSecs == 60000 {
		exInfo := exchange.Info()
		hs := make([][2]int64, 0, len(holes))
		wids := make(map[int]int)
		for _, h := range holes {
			num := int((h[1] - h[0]) / tfMSecs)
			if num <= exInfo.Min1mHole {
				continue
			}
			cnt, _ := wids[num]
			wids[num] = cnt + 1
			hs = append(hs, h)
		}
		//if len(wids) > 0 {
		//	var as = make([]string, 0, len(wids))
		//	for k, v := range wids {
		//		as = append(as, fmt.Sprintf("%v:%v", k, v))
		//	}
		//	log.Info("hole widths", zap.Int32("sid", sid), zap.String("d", strings.Join(as, " ")))
		//}
		holes = hs
	}
	// 查询已记录的khole，进行合并
	ctx := context.Background()
	resHoles, err_ := q.GetKHoles(ctx, GetKHolesParams{Sid: sid, Timeframe: timeFrame})
	if err_ != nil {
		return errs.New(core.ErrDbReadFail, err_)
	}
	for _, h := range holes {
		resHoles = append(resHoles, &KHole{Sid: sid, Timeframe: timeFrame, Start: h[0], Stop: h[1]})
	}
	slices.SortFunc(resHoles, func(a, b *KHole) int {
		return int((a.Start - b.Start) / 1000)
	})
	merged := make([]*KHole, 0)
	delIDs := make([]int64, 0)
	var prev *KHole
	for _, h := range resHoles {
		if h.Start == h.Stop {
			if h.ID > 0 {
				delIDs = append(delIDs, h.ID)
			}
			continue
		}
		if prev == nil || prev.Stop < h.Start {
			//与前一个洞不连续，出现缺口
			merged = append(merged, h)
			prev = h
		} else {
			if h.Stop > prev.Stop {
				prev.Stop = h.Stop
			}
			if h.ID > 0 {
				delIDs = append(delIDs, h.ID)
			}
		}
	}
	// 将合并后的kholes更新或插入到数据库
	err = q.DelKHoleIDs(delIDs...)
	if err != nil {
		return err
	}
	var adds []AddKHolesParams
	for _, h := range merged {
		if h.ID == 0 {
			adds = append(adds, AddKHolesParams{Sid: h.Sid, Timeframe: h.Timeframe, Start: h.Start, Stop: h.Stop})
		} else {
			err_ = q.SetKHole(ctx, SetKHoleParams{ID: h.ID, Start: h.Start, Stop: h.Stop})
			if err_ != nil {
				return errs.New(core.ErrDbExecFail, err_)
			}
		}
	}
	if len(adds) > 0 {
		_, err_ = q.AddKHoles(ctx, adds)
		if err_ != nil {
			return errs.New(core.ErrDbExecFail, err_)
		}
	}
	return nil
}

func (q *Queries) updateBigHyper(sid int32, timeFrame string, startMS, endMS int64, klines []*banexg.Kline) *errs.Error {
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	aggTfs := map[string]bool{timeFrame: true}
	aggJobs := make([]*KlineAgg, 0)
	unFinishJobs := make([]*KlineAgg, 0)
	curMS := btime.TimeMS()
	for _, item := range aggList {
		if item.MSecs <= tfMSecs {
			//跳过过小维度；跳过无关的连续聚合
			continue
		}
		startAlignMS := utils.AlignTfMSecs(startMS, item.MSecs)
		endAlignMS := utils.AlignTfMSecs(endMS, item.MSecs)
		if _, ok := aggTfs[item.AggFrom]; ok && startAlignMS < endAlignMS {
			// startAlign < endAlign说明：插入的数据所属bar刚好完成
			aggTfs[item.TimeFrame] = true
			aggJobs = append(aggJobs, item)
		}
		unBarStartMs := utils.AlignTfMSecs(curMS, item.MSecs)
		if endAlignMS >= unBarStartMs && endMS >= endAlignMS {
			// 仅当数据涉及当前周期未完成bar时，才尝试更新；仅传入相关的bar，提高效率
			unFinishJobs = append(unFinishJobs, item)
		}
	}
	if len(unFinishJobs) > 0 {
		var err *errs.Error
		if len(klines) == 0 {
			// 取最大周期的对齐后第一个时间作为开始时间
			msecs := unFinishJobs[len(unFinishJobs)-1].MSecs
			startAlign := utils.AlignTfMSecs(startMS, msecs)
			klines, err = q.QueryOHLCV(sid, timeFrame, startAlign, endMS, 0, true)
			if err != nil {
				return err
			}
		}
		for _, item := range unFinishJobs {
			err = updateUnFinish(q, item, sid, timeFrame, startMS, endMS, klines)
			if err != nil {
				return err
			}
		}
	}
	if len(aggJobs) > 0 {
		for _, item := range aggJobs {
			err := q.refreshAgg(item, sid, startMS, endMS, "")
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (q *Queries) refreshAgg(item *KlineAgg, sid int32, orgStartMS, orgEndMS int64, aggFrom string) *errs.Error {
	tfMSecs := item.MSecs
	startMS := utils.AlignTfMSecs(orgStartMS, tfMSecs)
	endMS := utils.AlignTfMSecs(orgEndMS, tfMSecs)
	if startMS == endMS && endMS < orgStartMS {
		// 没有出现新的完成的bar数据，无需更新
		// 前2个相等，说明：插入的数据所属bar尚未完成。
		// start_ms < org_start_ms说明：插入的数据不是所属bar的第一个数据
		return nil
	}
	// 有可能startMs刚好是下一个bar的开始，前一个需要-1
	aggStart := startMS - tfMSecs
	oldStart, oldEnd := q.GetKlineRange(sid, item.TimeFrame)
	if oldStart > 0 && oldEnd > oldStart {
		// 避免出现空洞或数据错误
		aggStart = min(aggStart, oldEnd)
		endMS = max(endMS, oldStart)
	}
	if aggFrom == "" {
		aggFrom = item.AggFrom
	}
	tblName := "kline_" + aggFrom
	sql := fmt.Sprintf(`
select sid,"time"/%d*%d as atime,%s
from %s where sid=%d and time>=%v and time<%v
GROUP BY sid, 2 
ORDER BY sid, 2`, tfMSecs, tfMSecs, aggFields, tblName, sid, aggStart, endMS)
	finalSql := fmt.Sprintf(`
insert into %s (sid, time, open, high, low, close, volume)
%s %s`, item.Table, sql, klineInsConflict)
	_, err_ := q.db.Exec(context.Background(), finalSql)
	if err_ != nil {
		return errs.New(core.ErrDbReadFail, err_)
	}
	// 更新有效区间范围
	err := q.updateKLineRange(sid, item.TimeFrame, startMS, endMS)
	if err != nil {
		return err
	}
	// 搜索空洞，更新khole
	err = q.updateKHoles(sid, item.TimeFrame, startMS, endMS)
	if err != nil {
		return err
	}
	return nil
}

func NewKlineAgg(TimeFrame, Table, AggFrom, AggStart, AggEnd, AggEvery, CpsBefore, Retention string) *KlineAgg {
	msecs := int64(utils.TFToSecs(TimeFrame) * 1000)
	return &KlineAgg{TimeFrame, msecs, Table, AggFrom, AggStart, AggEnd, AggEvery, CpsBefore, Retention}
}

/*
GetDownTF

	获取指定周期对应的下载的时间周期。
	只有1m和1h允许下载并写入超表。其他维度都是由这两个维度聚合得到。
*/
func GetDownTF(timeFrame string) (string, *errs.Error) {
	secs := utils.TFToSecs(timeFrame)
	if secs >= core.SecsDay {
		if secs%core.SecsDay > 0 {
			return "", errs.NewMsg(core.ErrInvalidTF, "invalid tf: %s", timeFrame)
		}
		return "1d", nil
	} else if secs >= core.SecsHour {
		if secs%core.SecsHour > 0 {
			return "", errs.NewMsg(core.ErrInvalidTF, "invalid tf: %s", timeFrame)
		}
		return "1h", nil
	} else if secs >= core.SecsMin*15 {
		if secs%(core.SecsMin*15) > 0 {
			return "", errs.NewMsg(core.ErrInvalidTF, "invalid tf: %s", timeFrame)
		}
		return "15m", nil
	} else if secs < core.SecsMin || secs%core.SecsMin > 0 {
		return "", errs.NewMsg(core.ErrInvalidTF, "invalid tf: %s", timeFrame)
	}
	return "1m", nil
}

func (q *Queries) DelKInfo(sid int32, timeFrame string) *errs.Error {
	sql := fmt.Sprintf("delete from kinfo where sid=%v and timeframe=$1", sid)
	return q.Exec(sql, timeFrame)
}

func (q *Queries) DelKLines(sid int32, timeFrame string) *errs.Error {
	sql := fmt.Sprintf("delete from kline_%s where sid=%v", timeFrame, sid)
	return q.Exec(sql)
}

func (q *Queries) GetKlineRange(sid int32, timeFrame string) (int64, int64) {
	sql := fmt.Sprintf("select start,stop from kinfo where sid=%v and timeframe=$1 limit 1", sid)
	row := q.db.QueryRow(context.Background(), sql, timeFrame)
	var start, stop int64
	_ = row.Scan(&start, &stop)
	return start, stop
}

func (q *Queries) GetKlineRanges(sidList []int32, timeFrame string) map[int32][2]int64 {
	var texts = make([]string, len(sidList))
	for i, sid := range sidList {
		texts[i] = fmt.Sprintf("%v", sid)
	}
	sidText := strings.Join(texts, ", ")
	sql := fmt.Sprintf("select sid,start,stop from kinfo where timeframe=$1 and sid in (%v)", sidText)
	rows, err_ := q.db.Query(context.Background(), sql, timeFrame)
	if err_ != nil {
		return map[int32][2]int64{}
	}
	res := make(map[int32][2]int64)
	defer rows.Close()
	for rows.Next() {
		var start, stop int64
		var sid int32
		err_ = rows.Scan(&sid, &start, &stop)
		if err_ != nil {
			continue
		}
		res[sid] = [2]int64{start, stop}
	}
	return res
}

func (q *Queries) DelFactors(sid int32) *errs.Error {
	sql := fmt.Sprintf("delete from adj_factors where sid=%v or sub_id=%v", sid, sid)
	return q.Exec(sql)
}

func (q *Queries) DelKLineUn(sid int32, timeFrame string) *errs.Error {
	sql := fmt.Sprintf("delete from kline_un where sid=%v and timeframe=$1", sid)
	return q.Exec(sql, timeFrame)
}

func (q *Queries) DelKHoles(sid int32, timeFrame string) *errs.Error {
	sql := fmt.Sprintf("delete from khole where sid=%v and timeframe=$1", sid)
	return q.Exec(sql, timeFrame)
}

func (q *Queries) DelKHoleIDs(ids ...int64) *errs.Error {
	if len(ids) == 0 {
		return nil
	}
	var builder strings.Builder
	builder.WriteString("delete from khole where id in (")
	arr := make([]string, len(ids))
	for i, id := range ids {
		arr[i] = strconv.Itoa(int(id))
	}
	builder.WriteString(strings.Join(arr, ","))
	builder.WriteString(")")
	return q.Exec(builder.String())
}

func mapToItems[T any](rows pgx.Rows, err_ error, assign func() (T, []any)) ([]T, error) {
	if err_ != nil {
		return nil, err_
	}
	defer rows.Close()
	items := make([]T, 0)
	for rows.Next() {
		i, fields := assign()
		if err := rows.Scan(fields...); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

/*
SyncKlineTFs
检查各kline表的数据一致性，如果低维度数据比高维度多，则聚合更新到高维度
*/
func SyncKlineTFs() *errs.Error {
	log.Info("run kline data sync ...")
	sess, conn, err := Conn(nil)
	if err != nil {
		return err
	}
	defer conn.Release()
	err = syncKlineInfos(sess)
	if err != nil {
		return err
	}
	log.Info("try filling KLine Holes ...")
	return tryFillHoles(sess)
}

type KHoleExt struct {
	*KHole
	TfMSecs int64
}

func tryFillHoles(sess *Queries) *errs.Error {
	ctx := context.Background()
	holes, err_ := sess.ListKHoles(ctx)
	if err_ != nil {
		return errs.New(core.ErrDbReadFail, err_)
	}
	rows := make([]*KHoleExt, len(holes))
	for i, h := range holes {
		rows[i] = &KHoleExt{
			KHole:   h,
			TfMSecs: int64(utils.TFToSecs(h.Timeframe) * 1000),
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Sid != b.Sid {
			return a.Sid < b.Sid
		}
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.TfMSecs < b.TfMSecs
	})
	// 已填充需要删除的khole
	badIds := make([]int64, 0, len(rows)/10)
	curSid := int32(0)
	var exs *ExSymbol
	var editNum int
	var newHoles [][4]int64 // 需要新增的记录[]sid,tfMSecs,start,stop
	for _, row := range rows {
		if row.Sid != curSid {
			curSid = row.Sid
			exs = GetSymbolByID(curSid)
		}
		if exs == nil {
			log.Warn("symbol id invalid", zap.Int32("sid", curSid))
			continue
		}
		start, stop := row.Start, row.Stop
		// 这里本来有从小周期检查已填充则跳过大周期KHole的逻辑，但因较大周期无法从特小周期归集，故这里取消。
		// 每个周期应独立检索实际K线范围，确保范围正确
		updateKHole := func(newStart, newStop int64) bool {
			if newStart == 0 || newStop == 0 {
				return false
			}
			if newStart == start && newStop == stop {
				// 此区间被完全填充，添加到删除列表
				badIds = append(badIds, row.ID)
				return true
			}
			if newStart == start {
				start = newStop
			} else if newStop == stop {
				stop = newStart
			} else {
				// 被包含，删除当前，新增前后两个KHole
				badIds = append(badIds, row.ID)
				newHoles = append(newHoles, [4]int64{int64(row.Sid), row.TfMSecs, start, newStart},
					[4]int64{int64(row.Sid), row.TfMSecs, newStop, stop})
				return true
			}
			return false
		}
		// 先检查是否已存在
		oldStart, oldStop, err := sess.CalcKLineRange(exs.ID, row.Timeframe, start, stop)
		if err != nil {
			return err
		}
		if updateKHole(oldStart, oldStop) {
			continue
		}
		// 下载K线，同时也会归集更高周期K线
		saveNum, err := downOHLCV2DBRange(sess, exg.Default, exs, row.Timeframe, start, stop, 0, 0, nil)
		if err != nil {
			return err
		}
		// 查询实际更新的区间
		if saveNum == 0 {
			continue
		}
		resStart, resStop, err := sess.CalcKLineRange(exs.ID, row.Timeframe, start, stop)
		if err != nil {
			return err
		}
		if updateKHole(resStart, resStop) {
			continue
		}
		if start != row.Start || stop != row.Stop {
			// 此区间被更新
			editNum += 1
			err_ = sess.SetKHole(ctx, SetKHoleParams{ID: row.ID, Start: start, Stop: stop})
			if err_ != nil {
				return errs.New(core.ErrDbExecFail, err_)
			}
		}
	}
	// 删除已填充的id
	if len(badIds) > 0 {
		err := sess.DelKHoleIDs(badIds...)
		if err != nil {
			return err
		}
	}
	// 新增kHoles
	if len(newHoles) > 0 {
		var items = make([]AddKHolesParams, len(newHoles))
		for i, h := range newHoles {
			items[i] = AddKHolesParams{
				Sid:       int32(h[0]),
				Timeframe: utils.SecsToTF(int(h[1] / 1000)),
				Start:     h[2],
				Stop:      h[3],
			}
		}
		_, err_ = sess.AddKHoles(ctx, items)
		if err_ != nil {
			return errs.New(core.ErrDbExecFail, err_)
		}
	}
	log.Info(fmt.Sprintf("find kHoles %v, filled: %v, add: %v, edit: %v", len(holes),
		len(badIds), len(newHoles), editNum))
	return nil
}

type KInfoExt struct {
	KInfo
	TfMSecs int64
}

func syncKlineInfos(sess *Queries) *errs.Error {
	infos, err_ := sess.ListKInfos(context.Background())
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	// 显示进度条
	pgTotal := (len(infos) + len(aggList)) * 10
	pBar := utils.NewPrgBar(pgTotal, "sync tf")
	defer pBar.Close()
	// 加载计算的区间
	calcs := make(map[string]map[int32][2]int64)
	for _, agg := range aggList {
		ranges, err := sess.CalcKLineRanges(agg.TimeFrame)
		if err != nil {
			return err
		}
		calcs[agg.TimeFrame] = ranges
		pBar.Add(10)
	}
	infoList := make([]*KInfoExt, 0, len(infos))
	for _, info := range infos {
		infoList = append(infoList, &KInfoExt{
			KInfo:   *info,
			TfMSecs: int64(utils.TFToSecs(info.Timeframe) * 1000),
		})
	}
	slices.SortFunc(infoList, func(a, b *KInfoExt) int {
		return int(a.Sid - b.Sid)
	})
	var curSid int32
	tfMap := make(map[string]*KInfoExt)
	for _, info := range infoList {
		if info.Sid != curSid {
			if len(tfMap) > 0 {
				err := sess.syncKlineSid(curSid, tfMap, calcs)
				if err != nil {
					return err
				}
			}
			tfMap = make(map[string]*KInfoExt)
			curSid = info.Sid
		}
		tfMap[info.Timeframe] = info
		pBar.Add(1)
	}
	return sess.syncKlineSid(curSid, tfMap, calcs)
}

func (q *Queries) syncKlineSid(sid int32, tfMap map[string]*KInfoExt, calcs map[string]map[int32][2]int64) *errs.Error {
	var err *errs.Error
	var err_ error
	tfRanges := make(map[string][2]int64)
	ctx := context.Background()
	for _, agg := range aggList {
		var oldStart, oldEnd int64
		if info, ok := tfMap[agg.TimeFrame]; ok {
			oldStart, oldEnd = info.Start, info.Stop
		}
		if ranges, ok := calcs[agg.TimeFrame][sid]; ok {
			tfRanges[agg.TimeFrame] = ranges
			newStart, newEnd := ranges[0], ranges[1]
			err_ = nil
			if oldStart == 0 && oldEnd == 0 {
				_, err_ = q.AddKInfo(ctx, AddKInfoParams{
					Sid:       sid,
					Timeframe: agg.TimeFrame,
					Start:     newStart,
					Stop:      newEnd,
				})
			} else if newStart != oldStart || oldEnd != newEnd {
				err_ = q.SetKInfo(ctx, SetKInfoParams{
					Sid:       sid,
					Timeframe: agg.TimeFrame,
					Start:     newStart,
					Stop:      newEnd,
				})
			}
			if err_ != nil {
				return errs.New(core.ErrDbExecFail, err_)
			}
			// 更新KHoles，避免有空洞但未记录
			err = q.updateKHoles(sid, agg.TimeFrame, newStart, newEnd)
			if err != nil {
				return err
			}
		} else if oldStart > 0 {
			// 没有数据，但有范围记录
			err = q.DelKInfo(sid, agg.TimeFrame)
			if err != nil {
				return err
			}
		}
	}
	// 尝试从子区间聚合更新
	for _, agg := range aggList[1:] {
		if agg.AggFrom == "" {
			continue
		}
		subRange, ok := tfRanges[agg.AggFrom]
		if !ok {
			continue
		}
		subStart, subEnd := subRange[0], subRange[1]
		var curStart, curEnd int64
		if curRange, ok := tfRanges[agg.TimeFrame]; ok {
			curStart, curEnd = curRange[0], curRange[1]
		}
		tfMSecs := int64(utils.TFToSecs(agg.TimeFrame) * 1000)
		subAlignStart := utils.AlignTfMSecs(subStart, tfMSecs)
		subAlignEnd := utils.AlignTfMSecs(subEnd, tfMSecs)
		if subAlignStart < curStart {
			err = q.refreshAgg(agg, sid, subStart, min(subEnd, curStart), "")
			if err != nil {
				return err
			}
		}
		if subAlignEnd > curEnd {
			err = q.refreshAgg(agg, sid, max(curEnd, subStart), subEnd, "")
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func GetKlineAggs() []*KlineAgg {
	return aggList
}

/*
CalcAdjFactors 计算更新所有复权因子
*/
func CalcAdjFactors(args *config.CmdArgs) *errs.Error {
	exInfo := exg.Default.Info()
	err := LoadAllExSymbols()
	if err != nil {
		return err
	}
	if exInfo.ID == "china" {
		return calcChinaAdjFactors()
	} else {
		return errs.NewMsg(errs.CodeParamInvalid, "exchange %s dont support adjust factors", exInfo.ID)
	}
}

func calcChinaAdjFactors() *errs.Error {
	exchange := exg.Default
	_, err := LoadMarkets(exchange, false)
	if err != nil {
		return err
	}
	err = InitListDates()
	if err != nil {
		return err
	}
	sess, conn, err := Conn(nil)
	if err != nil {
		return err
	}
	defer conn.Release()
	err = calcCnFutureFactors(sess)
	if err != nil {
		return err
	}
	// 对于股票计算复权因子?
	log.Info("calc china adj_factors complete")
	return nil
}

func calcCnFutureFactors(sess *Queries) *errs.Error {
	items := GetExSymbols("china", banexg.MarketLinear)
	exsList := utils.ValsOfMap(items)
	sort.Slice(exsList, func(i, j int) bool {
		return exsList[i].Symbol < exsList[j].Symbol
	})
	var err *errs.Error
	// 保存当前品种日线各个合约的成交量，用于寻找主力合约
	dateSidVols := make(map[int64]map[int32]*banexg.Kline)
	lastCode := ""
	var lastExs *ExSymbol
	ctx := context.Background()
	saveAdjFactors := func() *errs.Error {
		if lastCode == "" {
			return nil
		}
		exs := &ExSymbol{
			Exchange: lastExs.Exchange,
			Market:   lastExs.Market,
			ExgReal:  lastExs.ExgReal,
			Symbol:   lastCode + "888", // 期货888结尾表示主力连续合约
			Combined: true,
		}
		err = EnsureSymbols([]*ExSymbol{exs})
		if err != nil {
			return err
		}
		// 删除旧的主力连续合约复权因子
		err_ := sess.DelAdjFactors(ctx, exs.ID)
		if err_ != nil {
			return errs.New(core.ErrDbExecFail, err_)
		}
		dates := utils.KeysOfMap(dateSidVols)
		sort.Slice(dates, func(i, j int) bool {
			return dates[i] < dates[j]
		})
		// 逐日寻找成交量最大的合约ID，并计算复权因子
		lastSid := int32(0)
		var adds []AddAdjFactorsParams
		var row *AddAdjFactorsParams
		for _, dateMS := range dates {
			if row != nil {
				row.StartMs = dateMS
				adds = append(adds, *row)
				row = nil
			}
			vols, _ := dateSidVols[dateMS]
			curSid := int32(0)
			var maxK *banexg.Kline
			for sid, k := range vols {
				if maxK == nil || k.Volume > maxK.Volume {
					maxK = k
					curSid = sid
				}
			}
			if curSid != lastSid {
				factor := float64(1)
				if lastSid > 0 {
					lastK, _ := vols[lastSid]
					if lastK != nil {
						factor = maxK.Close / lastK.Close
					} else {
						date := btime.ToDateStr(maxK.Time, "")
						it := GetSymbolByID(lastSid)
						log.Warn("last sid invalid", zap.String("code", it.Symbol),
							zap.Int32("sid", lastSid), zap.String("date", date))
						continue
					}
				}
				row = &AddAdjFactorsParams{
					Sid:    exs.ID,
					SubID:  curSid,
					Factor: factor,
				}
				if lastSid == 0 {
					row.StartMs = dateMS
					adds = append(adds, *row)
					row = nil
				}
				lastSid = curSid
			}
		}
		_, err_ = sess.AddAdjFactors(ctx, adds)
		if err_ != nil {
			return errs.New(core.ErrDbExecFail, err_)
		}
		return nil
	}
	// 对所有期货标的，按顺序获取日K，并按时间记录
	var pBar = utils.NewPrgBar(len(exsList), "future")
	defer pBar.Close()
	for _, exs := range exsList {
		pBar.Add(1)
		parts := utils2.SplitParts(exs.Symbol)
		if len(parts) > 1 && parts[1].Type == utils2.StrInt {
			p1Str := parts[1].Val
			p1num, _ := strconv.Atoi(p1Str[len(p1Str)-2:])
			if p1num == 0 || p1num > 12 {
				// 跳过000, 888, 999这些特殊后缀
				continue
			}
		}
		if lastCode != parts[0].Val {
			err = saveAdjFactors()
			if err != nil {
				return err
			}
			dateSidVols = make(map[int64]map[int32]*banexg.Kline)
			lastCode = parts[0].Val
			lastExs = exs
		}
		klines, err := sess.QueryOHLCV(exs.ID, "1d", 0, 0, 0, false)
		if err != nil {
			return err
		}
		for _, k := range klines {
			vols, _ := dateSidVols[k.Time]
			if vols == nil {
				vols = make(map[int32]*banexg.Kline)
				dateSidVols[k.Time] = vols
			}
			vols[exs.ID] = k
		}
	}
	return saveAdjFactors()
}
