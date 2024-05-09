package utils

import (
	"errors"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg"
	"strconv"
)

type TFOrigin struct {
	TFSecs     int
	OffsetSecs int
	Origin     string
}

var (
	tfSecsMap = map[string]int{"ws": 5}
	secsTfMap = map[int]string{5: "ws"}
	tfOrigins = []*TFOrigin{{604800, 345600, "1970-01-05"}}
)

func parseTimeFrame(timeframe string) (int, error) {
	if len(timeframe) < 2 {
		return 0, errors.New("timeframe string too short")
	}

	amountStr := timeframe[:len(timeframe)-1]
	unit := timeframe[len(timeframe)-1]

	amount, err := strconv.Atoi(amountStr)
	if err != nil {
		return 0, err
	}

	var scale int
	switch unit {
	case 'y', 'Y':
		scale = core.SecsYear
	case 'M':
		scale = core.SecsMon
	case 'w', 'W':
		scale = core.SecsWeek
	case 'd', 'D':
		scale = core.SecsDay
	case 'h', 'H':
		scale = core.SecsHour
	case 'm':
		scale = core.SecsMin
	case 's', 'S':
		scale = 1
	default:
		return 0, errors.New("timeframe unit " + string(unit) + " is not supported")
	}

	return amount * scale, nil
}

/*
TFToSecs
将时间周期转为秒
支持单位：s, m, h, d, M, Y
*/
func TFToSecs(timeFrame string) int {
	secs, ok := tfSecsMap[timeFrame]
	var err error
	if !ok {
		secs, err = parseTimeFrame(timeFrame)
		if err != nil {
			panic(err)
		}
		tfSecsMap[timeFrame] = secs
		secsTfMap[secs] = timeFrame
	}
	return secs
}

func GetTfAlignOrigin(secs int) (string, int) {
	for _, item := range tfOrigins {
		if secs < item.TFSecs {
			break
		}
		if secs%item.TFSecs == 0 {
			return item.Origin, item.OffsetSecs
		}
	}
	return "1970-01-01", 0
}

/*
AlignTfSecsOffset
将给定的10位秒级时间戳，转为指定时间周期下，的头部开始时间戳，使用指定偏移
*/
func AlignTfSecsOffset(timeSecs int64, tfSecs int, offset int) int64 {
	if timeSecs > 1000000000000 {
		panic("10 digit timestamp is require for AlignTfSecs")
	}
	tfSecs64 := int64(tfSecs)
	if offset == 0 {
		return timeSecs / tfSecs64 * tfSecs64
	}
	offset64 := int64(offset)
	return (timeSecs-offset64)/tfSecs64*tfSecs64 + offset64
}

/*
AlignTfSecs
将给定的10位秒级时间戳，转为指定时间周期下，的头部开始时间戳
*/
func AlignTfSecs(timeSecs int64, tfSecs int) int64 {
	_, offset := GetTfAlignOrigin(tfSecs)
	return AlignTfSecsOffset(timeSecs, tfSecs, offset)
}

/*
AlignTfMSecs
将给定的13位毫秒级时间戳，转为指定时间周期下，的头部开始时间戳
*/
func AlignTfMSecs(timeMSecs int64, tfMSecs int64) int64 {
	if timeMSecs < 1000000000000 {
		panic("13 digit timestamp is require for AlignTfMSecs")
	}
	if tfMSecs < 1000 {
		panic("milliseconds tfMSecs is require for AlignTfMSecs")
	}
	return AlignTfSecs(timeMSecs/1000, int(tfMSecs/1000)) * 1000
}

func AlignTfMSecsOffset(timeMSecs, tfMSecs, offset int64) int64 {
	if timeMSecs < 1000000000000 {
		panic("13 digit timestamp is require for AlignTfMSecs")
	}
	if tfMSecs < 1000 {
		panic("milliseconds tfMSecs is require for AlignTfMSecs")
	}
	return AlignTfSecsOffset(timeMSecs/1000, int(tfMSecs/1000), int(offset/1000)) * 1000
}

/*
SecsToTF
将时间周期的秒数，转为时间周期
*/
func SecsToTF(tfSecs int) string {
	timeFrame, ok := secsTfMap[tfSecs]
	if !ok {
		switch {
		case tfSecs >= core.SecsYear:
			timeFrame = strconv.Itoa(tfSecs/core.SecsYear) + "y"
		case tfSecs >= core.SecsMon:
			timeFrame = strconv.Itoa(tfSecs/core.SecsMon) + "M"
		case tfSecs >= core.SecsWeek:
			timeFrame = strconv.Itoa(tfSecs/core.SecsWeek) + "w"
		case tfSecs >= core.SecsDay:
			timeFrame = strconv.Itoa(tfSecs/core.SecsDay) + "d"
		case tfSecs >= core.SecsHour:
			timeFrame = strconv.Itoa(tfSecs/core.SecsHour) + "h"
		case tfSecs >= core.SecsMin:
			timeFrame = strconv.Itoa(tfSecs/core.SecsMin) + "m"
		case tfSecs >= 1:
			timeFrame = strconv.Itoa(tfSecs) + "s"
		default:
			panic("unsupport tfSecs:" + strconv.Itoa(tfSecs))
		}
		secsTfMap[tfSecs] = timeFrame
	}
	return timeFrame
}

/*
BuildOHLCV
从交易或子OHLC数组中，构建或更新更粗粒度OHLC数组。
arr: 子OHLC列表。
toTFSecs: 指定要构建的时间粒度，单位：毫秒
preFire: 提前触发构建完成的比率；
resOHLCV: 已有的待更新数组
fromTFSecs: 传入的arr子数组间隔，未提供时计算，单位：毫秒
*/
func BuildOHLCV(arr []*banexg.Kline, toTFMSecs int64, preFire float64, resOHLCV []*banexg.Kline, fromTFMSecs int64) ([]*banexg.Kline, bool) {
	_, offset := GetTfAlignOrigin(int(toTFMSecs / 1000))
	return BuildOHLCVOff(arr, toTFMSecs, preFire, resOHLCV, fromTFMSecs, int64(offset*1000))
}

/*
BuildOHLCVOff
从交易或子OHLC数组中，构建或更新更粗粒度OHLC数组。
arr: 子OHLC列表。
toTFMSecs: 指定要构建的时间粒度，单位：毫秒
preFire: 提前触发构建完成的比率；
resOHLCV: 已有的待更新数组
fromTFSecs: 传入的arr子数组间隔，未提供时计算，单位：毫秒
alignOffMS: 对齐时间的偏移
*/
func BuildOHLCVOff(arr []*banexg.Kline, toTFMSecs int64, preFire float64, resOHLCV []*banexg.Kline, fromTFMSecs, alignOffMS int64) ([]*banexg.Kline, bool) {
	offsetMS := int64(float64(toTFMSecs) * preFire)
	subNum := len(arr)
	if fromTFMSecs == 0 && subNum >= 2 {
		fromTFMSecs = arr[subNum-1].Time - arr[subNum-2].Time
	}
	var big *banexg.Kline
	aggNum, cacheNum := 0, 0
	if fromTFMSecs > 0 {
		aggNum = int(toTFMSecs / fromTFMSecs) // 大周期由几个小周期组成
		cacheNum = len(arr)/aggNum + 3
	}
	if resOHLCV == nil {
		resOHLCV = make([]*banexg.Kline, 0, cacheNum)
	} else if len(resOHLCV) > 0 {
		big = resOHLCV[len(resOHLCV)-1]
	}
	aggCnt := 0 // 当前大周期bar从小周期聚合的数量
	for _, bar := range arr {
		timeAlign := AlignTfMSecsOffset(bar.Time+offsetMS, toTFMSecs, alignOffMS)
		if big != nil && big.Time == timeAlign {
			// 属于同一个
			if bar.Volume > 0 {
				if big.Volume == 0 {
					big.Open = bar.Open
					big.High = bar.High
					big.Low = bar.Low
				} else {
					if bar.High > big.High {
						big.High = bar.High
					}
					if bar.Low < big.Low {
						big.Low = bar.Low
					}
				}
				big.Close = bar.Close
				big.Volume += bar.Volume
			}
			aggCnt += 1
		} else {
			if aggCnt > aggNum {
				aggNum = aggCnt
			}
			if big != nil {
				if big.Volume > 0 || aggCnt*5 > aggNum {
					// 跳过小周期数量不足20%，且总成交量为0的
					resOHLCV = append(resOHLCV, big)
				}
			}
			big = bar.Clone() // 不修改原始数据
			big.Time = timeAlign
			aggCnt = 1
		}
	}
	lastFinished := false
	if fromTFMSecs > 0 && len(resOHLCV) > 0 {
		// 判断最后一个bar是否结束：假定arr中每个bar间隔相等，最后一个bar+间隔属于下一个规划区间，则认为最后一个bar结束
		finishMS := AlignTfMSecsOffset(arr[subNum-1].Time+fromTFMSecs+offsetMS, toTFMSecs, alignOffMS)
		lastFinished = finishMS > resOHLCV[len(resOHLCV)-1].Time
	}
	return resOHLCV, lastFinished
}
