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
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
	"slices"
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
	tfSecs := utils.TFToSecs(timeframe)
	tfMSecs := int64(tfSecs * 1000)
	maxEndMs := endMs
	if limit > 0 && startMs > 0 {
		endMs = min(startMs+tfMSecs*int64(limit), endMs)
	}
	curMs := btime.TimeMS()
	finishEndMS := utils.AlignTfMSecs(endMs, tfMSecs)
	unFinishMS := utils.AlignTfMSecs(curMs, tfMSecs)
	if finishEndMS > unFinishMS {
		finishEndMS = unFinishMS
	}
	dctSql := fmt.Sprintf(`
select time,open,high,low,close,volume from $tbl
where sid=%d and time >= %v and time < %v
order by time`, sid, startMs, finishEndMS)
	genGpSql := func() string {
		return fmt.Sprintf(`
				select %s from $tbl
                where sid=%d and time >= %v and time < %v
                group by gtime order by gtime`, colAggFields(timeframe, tfSecs), sid, startMs, finishEndMS)
	}
	rows, err_ := queryHyper(q, timeframe, dctSql, genGpSql)
	klines, err_ := mapToKlines(rows, err_)
	if err_ != nil {
		return nil, errs.New(core.ErrDbReadFail, err_)
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
	tfSecs := utils.TFToSecs(timeframe)
	tfMSecs := int64(tfSecs * 1000)
	if limit > 0 {
		endMs = min(startMs+tfMSecs*int64(limit), endMs)
	}
	curMs := btime.TimeMS()
	finishEndMS := utils.AlignTfMSecs(endMs, tfMSecs)
	unFinishMS := utils.AlignTfMSecs(curMs, tfMSecs)
	if finishEndMS > unFinishMS {
		finishEndMS = unFinishMS
	}
	sidTA := make([]string, len(sids))
	for i, id := range sids {
		sidTA[i] = fmt.Sprintf("(%v)", id)
	}
	sidText := strings.Join(sidTA, ", ")
	dctSql := fmt.Sprintf(`
select time,open,high,low,close,volume,sid from $tbl
where time >= %v and time < %v and sid in (values %v)
order by sid,time`, startMs, finishEndMS, sidText)
	genGpSql := func() string {
		return fmt.Sprintf(`
				select %s,sid from $tbl
                where time >= %v and time < %v and sid in (values %v)
                group by sid, gtime order by sid, gtime`, colAggFields(timeframe, tfSecs), startMs, finishEndMS, sidText)
	}
	rows, err_ := queryHyper(q, timeframe, dctSql, genGpSql)
	arrs, err_ := mapToItems(rows, err_, func() (*KlineSid, []any) {
		var i KlineSid
		return &i, []any{&i.Time, &i.Open, &i.High, &i.Low, &i.Close, &i.Volume, &i.Sid}
	})
	if err_ != nil {
		return errs.New(core.ErrDbReadFail, err_)
	}
	initCap := max(len(arrs)/len(sids), 16)
	var kline []*banexg.Kline
	curSid := int32(0)
	for _, k := range arrs {
		if k.Sid != curSid {
			if curSid > 0 && len(kline) > 0 {
				handle(curSid, kline)
			}
			curSid = k.Sid
			kline = make([]*banexg.Kline, 0, initCap)
		}
		kline = append(kline, &banexg.Kline{Time: k.Time, Open: k.Open, High: k.High, Low: k.Low,
			Close: k.Close, Volume: k.Volume})
	}
	if curSid > 0 && len(kline) > 0 {
		handle(curSid, kline)
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

func colAggFields(timeFrame string, tfSecs int) string {
	origin, _ := utils.GetTfAlignOrigin(tfSecs)
	return fmt.Sprintf("(extract(epoch from time_bucket('%s', time, origin => '%s')) * 1000)::bigint AS gtime,%s", timeFrame, origin, aggFields)
}

func queryHyper(sess *Queries, timeFrame, sql string, genSql func() string, args ...interface{}) (pgx.Rows, error) {
	agg, ok := aggMap[timeFrame]
	var table string
	if ok {
		table = agg.Table
	} else {
		// 时间帧没有直接符合的，从最接近的子timeframe聚合
		_, table = getSubTf(timeFrame)
		sql = genSql()
	}
	sql = strings.Replace(sql, "$tbl", table, 1)
	return sess.db.Query(context.Background(), sql, args...)
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
	_, err_ := q.db.Exec(context.Background(), sql)
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	return nil
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
	tfSecs := utils.TFToSecs(timeFrame)
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
		bigKlines, _ = utils.BuildOHLCV(klines, tfSecs, 0, nil, 0)
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
	toTfSecs := utils.TFToSecs(timeFrame)
	fromTfMSecs := int64(utils.TFToSecs(subTF) * 1000)
	merged, _ := utils.BuildOHLCV(arr, toTfSecs, 0, nil, fromTfMSecs)
	out := merged[0]
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
	_, err_ = sess.db.Exec(ctx, `insert into kline_un (sid, start_ms, stop_ms, open, high, low, close, volume, timeframe) 
values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, sub.Sid, sub.StartMs, endMS, sub.Open, sub.High, sub.Low,
		sub.Close, sub.Volume, sub.Timeframe)
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	return nil
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
	tblName := "kline_" + timeFrame
	var adds = make([]*KlineSid, len(arr))
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
应该在事务中调用此方法，否则插入k线后立刻读取计算关联信息没有最新数据
*/
func (q *Queries) InsertKLinesAuto(timeFrame string, sid int32, arr []*banexg.Kline) (int64, *errs.Error) {
	num, err := q.InsertKLines(timeFrame, sid, arr)
	if err != nil {
		return num, err
	}
	startMS := arr[0].Time
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	endMS := arr[len(arr)-1].Time + tfMSecs
	err = q.UpdateKRange(sid, timeFrame, startMS, endMS, arr)
	return num, err
}

/*
UpdateKRange
1. 更新K线的有效区间
2. 搜索空洞，更新Khole
3. 更新更大周期的连续聚合
*/
func (q *Queries) UpdateKRange(sid int32, timeFrame string, startMS, endMS int64, klines []*banexg.Kline) *errs.Error {
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
	// 更新更大的超表
	return q.updateBigHyper(sid, timeFrame, startMS, endMS, klines)
}

func (q *Queries) updateKLineRange(sid int32, timeFrame string, startMS, endMS int64) *errs.Error {
	// 更新有效区间范围
	tblName := "kline_" + timeFrame
	sql := fmt.Sprintf("select min(time),max(time) from %s where sid=%v", tblName, sid)
	ctx := context.Background()
	row := q.db.QueryRow(ctx, sql)
	var realStart, realEnd int64
	err_ := row.Scan(&realStart, &realEnd)
	if err_ != nil {
		return errs.New(core.ErrDbReadFail, err_)
	}
	if realStart == 0 || realEnd == 0 {
		return nil
	}
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	realStart = min(realStart, startMS)
	realEnd = max(realEnd+tfMSecs, endMS)
	oldStart, _ := q.GetKlineRange(sid, timeFrame)
	if oldStart == 0 {
		_, err_ = q.AddKInfo(ctx, AddKInfoParams{Sid: int64(sid), Timeframe: timeFrame, Start: realStart, Stop: realEnd})
	} else {
		err_ = q.SetKInfo(ctx, SetKInfoParams{Sid: int64(sid), Timeframe: timeFrame, Start: realStart, Stop: realEnd})
	}
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
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
		if endMS-prevTime > tfMSecs {
			holes = append(holes, [2]int64{prevTime + tfMSecs, endMS})
		}
	}
	if len(holes) == 0 {
		return nil
	}
	// 查询已记录的khole，进行合并
	ctx := context.Background()
	resHoles, err_ := q.GetKHoles(ctx, GetKHolesParams{Sid: int64(sid), Timeframe: timeFrame})
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
	delIDs := make([]int, 0)
	var prev *KHole
	for _, h := range resHoles {
		if h.Start == h.Stop {
			if h.ID > 0 {
				delIDs = append(delIDs, int(h.ID))
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
				delIDs = append(delIDs, int(h.ID))
			}
		}
	}
	// 将合并后的kholes更新或插入到数据库
	err = q.DelKHoles(delIDs...)
	if err != nil {
		return err
	}
	var adds []AddKHolesParams
	for _, h := range merged {
		if h.ID == 0 {
			adds = append(adds, AddKHolesParams{Sid: int64(h.Sid), Timeframe: h.Timeframe, Start: h.Start, Stop: h.Stop})
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
			klines, err = q.QueryOHLCV(sid, timeFrame, startMS, endMS, 0, true)
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

func (q *Queries) GetKlineRange(sid int32, timeFrame string) (int64, int64) {
	sql := fmt.Sprintf("select start,stop from kinfo where sid=%v and timeframe=$1 limit 1", sid)
	row := q.db.QueryRow(context.Background(), sql, timeFrame)
	var start, stop int64
	_ = row.Scan(&start, &stop)
	return start, stop
}

func (q *Queries) DelKHoles(ids ...int) *errs.Error {
	if len(ids) == 0 {
		return nil
	}
	var builder strings.Builder
	builder.WriteString("delete from khole where id in (")
	arr := make([]string, len(ids))
	for i, id := range ids {
		arr[i] = strconv.Itoa(id)
	}
	builder.WriteString(strings.Join(arr, ","))
	builder.WriteString(")")
	_, err_ := q.db.Exec(context.Background(), builder.String())
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	return nil
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
