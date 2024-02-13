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
)

func init() {
	time.Local = UTCLocale
}

/*
UTCTime
获取10位秒级浮点数
*/
func UTCTime() float64 {
	return float64(time.Now().UnixNano()) / float64(time.Second)
}

/*
UTCStamp
获取13位毫秒时间戳
*/
func UTCStamp() int64 {
	return time.Now().UnixMilli()
}

/*
Time
获取当前10位秒级时间戳
*/
func Time() float64 {
	if core.LiveMode() {
		return UTCTime()
	} else {
		if CurTimeMS == 0 {
			CurTimeMS = UTCStamp()
		}
		return float64(CurTimeMS) * 0.001
	}
}

/*
TimeMS
获取当前13位毫秒时间戳
*/
func TimeMS() int64 {
	if core.LiveMode() {
		return UTCStamp()
	} else {
		if CurTimeMS == 0 {
			CurTimeMS = UTCStamp()
		}
		return CurTimeMS
	}
}

func MSToTime(timeMSecs int64) *time.Time {
	seconds := timeMSecs / 1000
	nanos := (timeMSecs % 1000) * 1000000
	res := time.Unix(seconds, nanos).UTC()
	return &res
}

func Now() *time.Time {
	if !core.LiveMode() {
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
将时间字符串转为13位毫秒时间戳
支持的形式：
2006
20060102
10位时间戳
13位时间戳
2006-01-02 15:04
2006-01-02 15:04:05
*/
func ParseTimeMS(timeStr string) int64 {
	textLen := len(timeStr)
	digitNum := CountDigit(timeStr)
	if textLen == 4 && digitNum == 4 {
		return dateToMS("2006", timeStr)
	} else if textLen == 6 && digitNum == 6 {
		return dateToMS("200601", timeStr)
	} else if textLen == 8 && digitNum == 8 {
		return dateToMS("20060102", timeStr)
	} else if textLen == 10 && digitNum == 10 {
		// 10位时间戳
		secs, err := strconv.ParseInt(timeStr, 10, 64)
		if err != nil {
			panic(err)
		}
		return secs * int64(1000)
	} else if textLen == 13 && digitNum == 13 {
		msecs, err := strconv.ParseInt(timeStr, 10, 64)
		if err != nil {
			panic(err)
		}
		return msecs
	} else if textLen == 16 && digitNum == 12 {
		return dateToMS("2006-01-02 15:04", timeStr)
	} else if textLen == 19 && digitNum == 14 {
		return dateToMS("2006-01-02 15:04:05", timeStr)
	}
	panic(fmt.Errorf("unSupport date fmt: %s", timeStr))
}

func dateToMS(layout, timeStr string) int64 {
	t, err := time.Parse(layout, timeStr)
	if err != nil {
		panic(fmt.Errorf("parse %s fail: %s", layout, timeStr))
	}
	return t.UnixMilli()
}

/*
ToDateStr
将时间戳转为时间字符串
*/
func ToDateStr(timestamp int64, format string) string {
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

	if format == "" {
		format = "2006-01-02 15:04:05"
	}
	return t.Format(format)
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
