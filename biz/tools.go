package biz

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/csv"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func LoadZipKline(inPath string, fid int, file *zip.File, arg interface{}) *errs.Error {
	cleanName := strings.Split(filepath.Base(file.Name), ".")[0]
	exArgs := arg.([]string)
	exgName, market := exArgs[0], exArgs[1]
	exchange, err := exg.GetWith(exgName, market, exArgs[2])
	if err != nil {
		return err
	}
	yearStr := strings.Split(filepath.Base(inPath), ".")[0]
	year, _ := strconv.Atoi(yearStr)
	mar, err := exchange.MapMarket(cleanName, year)
	if err != nil {
		log.Warn("symbol invalid", zap.String("id", cleanName), zap.Error(err))
		return nil
	}
	exs := &orm.ExSymbol{Symbol: mar.Symbol, Exchange: exgName, ExgReal: mar.ExgReal, Market: market}
	err = orm.EnsureSymbols([]*orm.ExSymbol{exs})
	if err != nil {
		return err
	}
	fReader, err_ := file.Open()
	if err_ != nil {
		return errs.New(errs.CodeIOReadFail, err_)
	}
	rows, err_ := csv.NewReader(fReader).ReadAll()
	if err_ != nil {
		return errs.New(errs.CodeIOReadFail, err_)
	}
	if len(rows) <= 1 {
		return nil
	}
	klines := make([]*banexg.Kline, 0, len(rows))
	lastMS := int64(0)
	tfMSecs := int64(math.MaxInt64)
	for _, r := range rows {
		barTime, _ := strconv.ParseInt(r[0], 10, 64)
		o, _ := strconv.ParseFloat(r[1], 64)
		h, _ := strconv.ParseFloat(r[2], 64)
		l, _ := strconv.ParseFloat(r[3], 64)
		c, _ := strconv.ParseFloat(r[4], 64)
		v, _ := strconv.ParseFloat(r[5], 64)
		if barTime == 0 {
			continue
		}
		timeDiff := barTime - lastMS
		lastMS = barTime
		if timeDiff > 0 && timeDiff < tfMSecs {
			tfMSecs = timeDiff
		}
		klines = append(klines, &banexg.Kline{
			Time:   barTime,
			Open:   o,
			High:   h,
			Low:    l,
			Close:  c,
			Volume: v,
		})
	}
	sort.Slice(klines, func(i, j int) bool {
		return klines[i].Time < klines[j].Time
	})
	startMS, endMS := klines[0].Time, klines[len(klines)-1].Time
	timeFrame := utils.SecsToTF(int(tfMSecs / 1000))
	timeFrame, err = orm.GetDownTF(timeFrame)
	if err != nil {
		log.Error("get down tf fail", zap.Int64("ms", tfMSecs), zap.String("id", exs.Symbol),
			zap.String("path", inPath), zap.Error(err))
		return nil
	}
	tfMSecs = int64(utils.TFToSecs(timeFrame) * 1000)
	ctx := context.Background()
	sess, conn, err := orm.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	// 过滤非交易时间段，成交量为0的
	// 由于历史数据中部分交易日未录入，故不适用交易日历过滤K线
	holes, err := sess.GetExSHoles(exchange, exs, startMS, endMS, true)
	if err != nil {
		return err
	}
	holeNum := len(holes)
	if holeNum > 0 {
		newKlines := make([]*banexg.Kline, 0, len(klines))
		hi := 0
		var half *banexg.Kline
		unExpNum := 0
		dayMSecs := int64(utils.TFToSecs("1d") * 1000)
		for i, k := range klines {
			for hi < holeNum && holes[hi][1] <= k.Time {
				if unExpNum > 0 {
					h := holes[hi]
					if h[1]-h[0] >= dayMSecs {
						// 非交易时间段超过1天
						expNum := int((h[1] - h[0]) / tfMSecs)
						if unExpNum*20 > expNum {
							// 非交易时间内，有效bar数量至少5%，则输出警告
							startStr := btime.ToDateStr(h[0], "")
							endStr := btime.ToDateStr(h[1], "")
							log.Warn("find klines in non-trade time", zap.Int32("sid", exs.ID),
								zap.Int("num", unExpNum), zap.String("start", startStr),
								zap.String("end", endStr))
						} else if unExpNum > 20 {
							half = nil
						}
					}
					unExpNum = 0
				}
				hi += 1
			}
			if hi >= holeNum {
				newKlines = append(newKlines, klines[i:]...)
				break
			}
			if half != nil {
				// 有前面额外的，合并到一起
				if k.High < half.High {
					k.High = half.High
				}
				if k.Low > half.Low {
					k.Low = half.Low
				}
				k.Open = half.Open
				k.Volume += half.Volume
				half = nil
			}
			h := holes[hi]
			if k.Time < h[0] {
				// 有效时间内
				newKlines = append(newKlines, k)
			} else if k.Volume > 0 {
				// 非交易时间段内，但有成交量，合并到最近有效bar
				unExpNum += 1
				if h[1]-k.Time < k.Time-h[0] {
					//离后面更近，合并到下一个
					if h[1]-k.Time < tfMSecs*10 {
						half = k
					}
				} else if len(newKlines) > 0 {
					// 离前面更近，合并到上一个（最多10个）
					p := newKlines[len(newKlines)-1]
					if k.Time-p.Time < tfMSecs*10 {
						if p.High < k.High {
							p.High = k.High
						}
						if p.Low > k.Low {
							p.Low = k.Low
						}
						p.Close = k.Close
						p.Volume += k.Volume
					}
				}
			}
		}
		if len(newKlines) == 0 {
			return nil
		}
		klines = newKlines
	}
	oldStart, oldEnd := sess.GetKlineRange(exs.ID, timeFrame)
	if oldStart <= startMS && endMS <= oldEnd {
		// 都已存在，无需写入
		return nil
	}
	if oldStart > 0 {
		newKlines := make([]*banexg.Kline, 0, len(klines))
		for _, k := range klines {
			if k.Time < oldStart || k.Time >= oldEnd {
				newKlines = append(newKlines, k)
			}
		}
		if len(newKlines) == 0 {
			return nil
		}
		klines = newKlines
	}
	// 这里不自动归集，因有些bar成交量为0，不可使用数据库默认的归集策略；应调用BuildOHLCVOff归集
	num, err := sess.InsertKLinesAuto(timeFrame, exs.ID, klines, false)
	if err == nil && num > 0 {
		startDt := btime.ToDateStr(startMS, "")
		endDt := btime.ToDateStr(endMS, "")
		log.Info("insert klines", zap.String("symbol", exs.Symbol), zap.Int32("sid", exs.ID),
			zap.Int64("num", num), zap.String("start", startDt), zap.String("end", endDt))
		// 插入更大周期
		aggList := orm.GetKlineAggs()
		for _, agg := range aggList[1:] {
			if agg.MSecs <= tfMSecs {
				continue
			}
			offMS := int64(exg.GetAlignOff(exchange.GetID(), int(agg.MSecs/1000)) * 1000)
			klines, _ = utils.BuildOHLCVOff(klines, agg.MSecs, 0, nil, tfMSecs, offMS)
			if len(klines) == 0 {
				continue
			}
			num, err = sess.InsertKLinesAuto(agg.TimeFrame, exs.ID, klines, false)
			if err != nil {
				log.Error("insert kline fail", zap.String("id", mar.ID),
					zap.String("tf", agg.TimeFrame), zap.Error(err))
			}
			if num == 0 {
				break
			}
		}
	}
	return err
}

func LoadCalendars(args *config.CmdArgs) *errs.Error {
	core.SetRunMode(core.RunModeOther)
	err := SetupComs(args)
	if err != nil {
		return err
	}
	if args.InPath == "" {
		return errs.NewMsg(errs.CodeParamRequired, "--in is required")
	}
	file, err_ := os.Open(args.InPath)
	if err_ != nil {
		return errs.New(errs.CodeIOReadFail, err_)
	}
	defer file.Close()
	rows, err_ := csv.NewReader(bufio.NewReader(file)).ReadAll()
	if err_ != nil {
		return errs.New(errs.CodeIOReadFail, err_)
	}
	ctx := context.Background()
	sess, conn, err := orm.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	lastExg := ""
	dateList := make([][2]int64, 0)
	dtLay := "2006-01-02"
	for _, row := range rows {
		startMS := btime.ParseTimeMSBy(dtLay, row[1])
		stopMS := btime.ParseTimeMSBy(dtLay, row[2])
		if lastExg == "" {
			lastExg = row[0]
		}
		if lastExg != row[0] {
			if len(dateList) > 0 {
				err = sess.SetCalendars(lastExg, dateList)
				if err != nil {
					log.Error("save calendars fail", zap.String("exg", lastExg), zap.Error(err))
				}
				dateList = make([][2]int64, 0)
			}
			lastExg = row[0]
		}
		dateList = append(dateList, [2]int64{startMS, stopMS})
	}
	if len(dateList) > 0 {
		err = sess.SetCalendars(lastExg, dateList)
		if err != nil {
			log.Error("save calendars fail", zap.String("exg", lastExg), zap.Error(err))
		}
	}
	log.Info("load calendars success", zap.Int("num", len(rows)))
	return nil
}
