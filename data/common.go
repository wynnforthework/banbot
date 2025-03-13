package data

import (
	"context"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	"go.uber.org/zap"
	"sync"
)

type periodSta struct {
	stamps map[int32]int64
	lock   sync.Mutex
	msecs  int64
}

func newPeriodSta(tf string) *periodSta {
	msecs := int64(utils2.TFToSecs(tf) * 1000)
	return &periodSta{
		stamps: make(map[int32]int64),
		msecs:  msecs,
	}
}

// 返回指定周期对齐时间戳，此周期已入库的最新bar时间戳
func (p *periodSta) alignAndLast(sess *orm.Queries, sid int32, tf string, curMS int64) (int64, int64) {
	p.lock.Lock()
	// 上一个小时对齐时间戳
	lastMS, _ := p.stamps[sid]
	if lastMS == 0 {
		kinfos, _ := sess.FindKInfos(context.Background(), sid)
		for _, kinfo := range kinfos {
			if kinfo.Timeframe == tf {
				lastMS = kinfo.Stop - p.msecs
				break
			}
		}
	}
	hourAlign := utils2.AlignTfMSecs(curMS, p.msecs)
	if lastMS == 0 {
		lastMS = hourAlign - p.msecs
	}
	if hourAlign > lastMS {
		p.stamps[sid] = hourAlign
	}
	p.lock.Unlock()
	return hourAlign, lastMS
}

func (p *periodSta) reset(sid int32, timeMS int64) {
	p.lock.Lock()
	p.stamps[sid] = timeMS
	p.lock.Unlock()
}

func trySaveKlines(job *SaveKline, tfSecs int, mntSta *periodSta, hourSta *periodSta) {
	ctx := context.Background()
	sess, conn, err := orm.Conn(ctx)
	if err == nil {
		defer conn.Release()
		var addBars = job.Arr
		endMS := addBars[len(addBars)-1].Time + int64(tfSecs*1000)
		if tfSecs < 60 {
			// 最小保存1m级别k线
			mntAlign, prevMS := mntSta.alignAndLast(sess, job.Sid, "1m", endMS)
			if mntAlign <= prevMS {
				// 未出现新的1m完成k线
				return
			}
			addBars, err = downSidKline(sess, job.Sid, "1m", prevMS, mntAlign)
			if err != nil {
				mntSta.reset(job.Sid, prevMS)
				log.Error("down kline 1m fail", zap.Int32("sid", job.Sid), zap.Error(err))
				return
			}
		} else {
			// 1m级别，可直接入库
			initMutex.Lock()
			_, ok := initSids[job.Sid]
			initMutex.Unlock()
			if !ok {
				var nextMS int64
				nextMS, err = fillPrevHole(sess, job, addBars)
				var cutIdx = 0
				for i, bar := range addBars {
					if bar.Time < nextMS {
						cutIdx = i + 1
					} else {
						break
					}
				}
				addBars = addBars[cutIdx:]
			}
			if err == nil && len(addBars) > 0 {
				_, err = sess.InsertKLinesAuto(job.TimeFrame, job.Sid, addBars, true)
			}
		}
		if err == nil && len(addBars) > 0 {
			// 下载1h及以上周期K线数据
			hourAlign, lastMS := hourSta.alignAndLast(sess, job.Sid, "1h", endMS)
			if hourAlign > lastMS {
				_, err = downSidKline(sess, job.Sid, "1h", lastMS, hourAlign)
			}
		}
	}
	if err != nil {
		log.Error("consumeWriteQ: fail", zap.Int32("sid", job.Sid), zap.Error(err))
	}
}

func downSidKline(sess *orm.Queries, sid int32, tf string, startMs int64, endMs int64) ([]*banexg.Kline, *errs.Error) {
	var err *errs.Error
	var num int
	exs := orm.GetSymbolByID(sid)
	if exs == nil {
		log.Error("sid not found in cache", zap.Int32("sid", sid))
	} else {
		var exchange banexg.BanExchange
		exchange, err = exg.GetWith(exs.Exchange, exs.Market, "")
		if err == nil {
			num, err = sess.DownOHLCV2DB(exchange, exs, tf, startMs, endMs, nil)
			if err == nil && num > 0 {
				return sess.QueryOHLCV(sid, tf, startMs, endMs, 0, false)
			}
		}
	}
	return nil, err
}

/*
Check whether there are any missing K lines, and automatically query and update if there are any.
检查是否有缺失的K线，有则自动查询更新（一般在刚启动时，收到的爬虫推送1mK线不含前面的，需要下载前面的并保存到WaitBar中）
*/
func (j *PairTFCache) fillLacks(pair string, subTfSecs int, startMS, endMS int64) ([]*banexg.Kline, *errs.Error) {
	if j.NextMS == 0 || j.NextMS >= startMS {
		j.NextMS = endMS
		return nil, nil
	}
	// 这里NextMS < startMS，出现了bar缺失，查询更新。
	exs, err := orm.GetExSymbolCur(pair)
	if err != nil {
		return nil, err
	}
	exchange := exg.Default
	if !exchange.HasApi(banexg.ApiFetchOHLCV, exs.Market) {
		// Downloading K lines is currently not allowed, skip
		// 当前不允许下载K线，跳过
		j.NextMS = endMS
		return nil, nil
	}
	fetchTF := utils2.SecsToTF(subTfSecs)
	tfMSecs := int64(j.TFSecs * 1000)
	bigStartMS := utils2.AlignTfMSecs(j.NextMS, tfMSecs)
	_, preBars, err := orm.AutoFetchOHLCV(exchange, exs, fetchTF, bigStartMS, startMS, 0, false, nil)
	if err != nil {
		return nil, err
	}
	var doneBars []*banexg.Kline
	j.WaitBar = nil
	if len(preBars) > 0 {
		fromTFMS := int64(subTfSecs * 1000)
		oldBars, _ := utils.BuildOHLCV(preBars, tfMSecs, 0, nil, fromTFMS, j.AlignOffMS)
		if len(oldBars) > 0 {
			j.WaitBar = oldBars[len(oldBars)-1]
			doneBars = oldBars[:len(oldBars)-1]
		}
	}
	j.NextMS = endMS
	return doneBars, nil
}
