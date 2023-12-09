package utils

import (
	"errors"
	"github.com/anyongjin/banbot/core"
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
		scale = 60 * 60 * 24 * 365
	case 'M':
		scale = 60 * 60 * 24 * 30
	case 'w', 'W':
		scale = 60 * 60 * 24 * 7
	case 'd', 'D':
		scale = 60 * 60 * 24
	case 'h', 'H':
		scale = 60 * 60
	case 'm':
		scale = 60
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
func AlignTfMSecs(timeMSecs int64, tfMSecs int) int64 {
	if timeMSecs < 1000000000000 {
		panic("13 digit timestamp is require for AlignTfMSecs")
	}
	if tfMSecs < 1000 {
		panic("milliseconds tfMSecs is require for AlignTfMSecs")
	}
	return AlignTfSecs(timeMSecs/int64(1000), tfMSecs/1000) * int64(1000)
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
