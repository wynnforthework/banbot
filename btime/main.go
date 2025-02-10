package btime

import (
	"fmt"
	"github.com/banbox/banbot/core"
	"strconv"
	"time"
	"unicode"
)

var (
	CurTimeMS    = int64(0)
	UTCLocale, _ = time.LoadLocation("UTC")
	LocShow      *time.Location // 用于显示的时区
)

func init() {
	time.Local = UTCLocale
}

/*
UTCTime
Get 10-digit second-level floating point number
获取10位秒级浮点数
*/
func UTCTime() float64 {
	return float64(time.Now().UnixNano()) / float64(time.Second)
}

/*
UTCStamp
Get 13-digit millisecond timestamp
获取13位毫秒时间戳
*/
func UTCStamp() int64 {
	return time.Now().UnixMilli()
}

/*
Time
Get the current 10-digit second-level timestamp
获取当前10位秒级时间戳
*/
func Time() float64 {
	if core.BackTestMode {
		if CurTimeMS == 0 {
			CurTimeMS = UTCStamp()
		}
		return float64(CurTimeMS) * 0.001
	} else {
		return UTCTime()
	}
}

/*
TimeMS
Get the current 13-digit millisecond timestamp
获取当前13位毫秒时间戳
*/
func TimeMS() int64 {
	if core.BackTestMode {
		if CurTimeMS == 0 {
			CurTimeMS = UTCStamp()
		}
		return CurTimeMS
	} else {
		return UTCStamp()
	}
}

func MSToTime(timeMSecs int64) *time.Time {
	seconds := timeMSecs / 1000
	nanos := (timeMSecs % 1000) * 1000000
	res := time.Unix(seconds, nanos).UTC()
	return &res
}

func Now() *time.Time {
	if core.BackTestMode {
		if CurTimeMS == 0 {
			CurTimeMS = UTCStamp()
		}
		return MSToTime(CurTimeMS)
	}
	res := time.Now().In(UTCLocale)
	return &res
}

/*
ParseTimeMS
Convert time string to 13-digit millisecond timestamp
Supported forms:
将时间字符串转为13位毫秒时间戳
支持的形式：
2006
20060102
10-digit timestamp
13-digit timestamp
2006-01-02 15:04
2006-01-02 15:04:05
*/
func ParseTimeMS(timeStr string) (int64, error) {
	textLen := len(timeStr)
	digitNum := CountDigit(timeStr)
	if textLen == 4 && digitNum == 4 {
		return ParseTimeMSBy("2006", timeStr), nil
	} else if textLen == 6 && digitNum == 6 {
		return ParseTimeMSBy("200601", timeStr), nil
	} else if textLen == 8 && digitNum == 8 {
		return ParseTimeMSBy("20060102", timeStr), nil
	} else if textLen == 10 && digitNum == 10 {
		// 10位时间戳
		secs, err := strconv.ParseInt(timeStr, 10, 64)
		if err != nil {
			panic(err)
		}
		return secs * int64(1000), nil
	} else if textLen == 13 && digitNum == 13 {
		msecs, err := strconv.ParseInt(timeStr, 10, 64)
		if err != nil {
			panic(err)
		}
		return msecs, nil
	} else if textLen == 16 && digitNum == 12 {
		return ParseTimeMSBy("2006-01-02 15:04", timeStr), nil
	} else if textLen == 19 && digitNum == 14 {
		return ParseTimeMSBy(core.DefaultDateFmt, timeStr), nil
	}
	return 0, fmt.Errorf("unSupport date fmt: %s", timeStr)
}

func ParseTimeMSBy(layout, timeStr string) int64 {
	t, err := time.Parse(layout, timeStr)
	if err != nil {
		panic(fmt.Errorf("parse %s fail: %s", layout, timeStr))
	}
	return t.UnixMilli()
}

/*
ToDateStr
Convert timestamp to time string
将时间戳转为时间字符串
format default: 2006-01-02 15:04:05
*/
func ToDateStr(timestamp int64, format string) string {
	t := ToTime(timestamp)
	if format == "" {
		format = core.DefaultDateFmt
	}
	return t.Format(format)
}

func ToDateStrLoc(timestamp int64, format string) string {
	t := ToTime(timestamp).In(LocShow)
	if format == "" {
		format = core.DefaultDateFmt
	}
	return t.Format(format)
}

func ToTime(timestamp int64) time.Time {
	var t time.Time
	if timestamp > 1000000000000 {
		// 13位毫秒时间戳
		seconds := timestamp / 1000             // 秒
		nanoseconds := (timestamp % 1000) * 1e6 // 毫秒转为纳秒
		t = time.Unix(seconds, nanoseconds)
	} else {
		// 10位秒级时间戳
		t = time.Unix(timestamp, 0)
	}
	return t
}

func CountDigit(text string) int {
	count := 0
	for _, c := range text {
		if unicode.IsDigit(c) {
			count += 1
		}
	}
	return count
}
