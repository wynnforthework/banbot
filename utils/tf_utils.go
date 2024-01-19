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
	if !ok {
		secs, err := parseTimeFrame(timeFrame)
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
AlignTfSecs
将给定的10位秒级时间戳，转为指定时间周期下，的头部开始时间戳
*/
func AlignTfSecs(timeSecs int64, tfSecs int) int64 {
	if timeSecs > 1000000000000 {
		panic("10 digit timestamp is require for AlignTfSecs")
	}
	originOff := 0
	for _, item := range tfOrigins {
		if tfSecs < item.TFSecs {
			break
		}
		if tfSecs%item.TFSecs == 0 {
			originOff = item.OffsetSecs
			break
		}
	}
	if originOff > 0 {
		return timeSecs / int64(tfSecs) * int64(tfSecs)
	}
	return (timeSecs-int64(originOff))/int64(tfSecs)*int64(tfSecs) + int64(originOff)
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
	return AlignTfSecs(timeMSecs/int64(1000), int(tfMSecs/1000)) * int64(1000)
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
toTFSecs: 指定要构建的时间粒度，单位：秒
preFire: 提前触发构建完成的比率；
resOHLCV: 已有的待更新数组
fromTFSecs: 传入的arr子数组间隔，未提供时计算
*/
func BuildOHLCV(arr []*banexg.Kline, toTFSecs int, preFire float64, resOHLCV []*banexg.Kline, fromTFMSecs int64) ([]*banexg.Kline, bool) {
	tfMSecs := int64(toTFSecs * 1000)
	offsetMS := int64(float64(tfMSecs) * preFire)
	var prevKline *banexg.Kline
	if resOHLCV == nil {
		resOHLCV = make([]*banexg.Kline, 0)
	} else if len(resOHLCV) > 0 {
		prevKline = resOHLCV[len(resOHLCV)-1]
	}
	rawMS := make([]int64, 0, len(arr))
	for _, bar := range arr {
		rawMS = append(rawMS, bar.Time)
		bar.Time = AlignTfMSecs(bar.Time+offsetMS, tfMSecs)
		if prevKline == nil || bar.Time >= prevKline.Time+tfMSecs {
			resOHLCV = append(resOHLCV, bar)
			prevKline = bar
		} else {
			prevKline.High = max(prevKline.High, bar.High)
			prevKline.Low = min(prevKline.Low, bar.Low)
			prevKline.Close = bar.Close
		}
	}
	lastFinished := false
	if fromTFMSecs == 0 && len(rawMS) >= 2 {
		// 至少有2个，判断最后一个bar是否结束：假定arr中每个bar间隔相等，最后一个bar+间隔属于下一个规划区间，则认为最后一个bar结束
		fromTFMSecs = rawMS[len(rawMS)-1] - rawMS[len(rawMS)-2]
	}
	if fromTFMSecs > 0 {
		finishMS := AlignTfMSecs(rawMS[len(rawMS)-1]+fromTFMSecs+offsetMS, tfMSecs)
		lastFinished = finishMS > resOHLCV[len(resOHLCV)-1].Time
	}
	return resOHLCV, lastFinished
}
