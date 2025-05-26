package orm

import (
	"encoding/gob"
	"errors"
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/log"
	"github.com/banbox/banexg/utils"
	"go.uber.org/zap"
	"io"
	"math"
	"os"
	"strings"
	"testing"
)

// TestKLineConsistency checks the consistency of K-line data in dump files
// It verifies:
// 1. Each K-line's time is within 2 minutes of its row time
// 2. The interval between consecutive K-lines matches the timeframe
// 3. Reports any missing K-lines
func TestKLineConsistency(t *testing.T) {
	inPath := "E:\\trade\\go\\tmp\\4h3.gob"
	file, err := os.Open(inPath)
	if err != nil {
		t.Fatalf("Failed to open dump file: %v", err)
	}
	defer file.Close()

	gob.Register(banexg.Kline{})
	gob.Register([]*DumpRow{})
	dec := gob.NewDecoder(file)

	err2 := initApp()
	if err2 != nil {
		panic(err2)
	}

	exgName, market := "binance", "linear"
	gpKlines := make(map[string][]*banexg.Kline) // map[pair_tf]klines
	endMS := int64(0)
	totalNum := 0
	printStat := func() {
		for key, arr := range gpKlines {
			envKeyArr := strings.Split(key, "_")
			symbol, tf := envKeyArr[0], envKeyArr[1]
			tfMSecs := int64(utils.TFToSecs(tf)) * 1000
			exs := GetExSymbol2(exgName, market, symbol)
			startMS := arr[0].Time
			_, kline, err2 := GetOHLCV(exs, tf, startMS, endMS, 0, false)
			if err2 != nil {
				t.Error("get ohlcv fail", symbol, tf, err2)
				continue
			}
			ida := 0
			ka := arr[0]
			for _, k := range kline {
				if k.Time < ka.Time {
					continue
				}
				for k.Time > ka.Time && ida+1 < len(arr) {
					// 本地有K线，实盘无K线；
					ida += 1
					ka = arr[ida]
				}
				if k.Time > ka.Time {
					// 没有更多实盘K线
					break
				}
				// 时间相同，比较K线是否相同
				openDiff := math.Abs(k.Open-ka.Open) / max(k.Open, ka.Open)
				highDiff := math.Abs(k.High-ka.High) / max(k.High, ka.High)
				lowDiff := math.Abs(k.Low-ka.Low) / max(k.Low, ka.Low)
				closeDiff := math.Abs(k.Close-ka.Close) / max(k.Close, ka.Close)
				volDiff := math.Abs(k.Volume-ka.Volume) / max(k.Volume, ka.Volume)
				infoDiff := math.Abs(k.Info-ka.Info) / max(k.Info, ka.Info)
				var fields []string
				if openDiff > 0.01 {
					fields = append(fields, fmt.Sprintf("open: %f - %f", k.Open, ka.Open))
				}
				if highDiff > 0.01 {
					fields = append(fields, fmt.Sprintf("high: %f - %f", k.High, ka.High))
				}
				if lowDiff > 0.01 {
					fields = append(fields, fmt.Sprintf("low: %f - %f", k.Low, ka.Low))
				}
				if closeDiff > 0.01 {
					fields = append(fields, fmt.Sprintf("close: %f - %f", k.Close, ka.Close))
				}
				if volDiff > 0.01 {
					fields = append(fields, fmt.Sprintf("vol: %f - %f", k.Volume, ka.Volume))
				}
				if infoDiff > 0.01 {
					fields = append(fields, fmt.Sprintf("info: %f - %f", k.Info, ka.Info))
				}
				if len(fields) > 0 {
					curDateStr := btime.ToDateStr(ka.Time, core.DefaultDateFmt)
					fmt.Printf("%s[%d] kline diff %s: %s\n", key, ida, curDateStr, strings.Join(fields, ", "))
				}
				ida += 1
				if ida < len(arr) {
					ka = arr[ida]
				} else {
					break
				}
			}
			num := (endMS - startMS) / tfMSecs
			checkLacks := func(bars []*banexg.Kline) []int {
				var flags = make([]int, num)
				for _, b := range bars {
					idx := int((b.Time - startMS) / tfMSecs)
					flags[idx] = 1
				}
				var res = make([]int, 0, num)
				for i, flag := range flags {
					if flag == 0 {
						res = append(res, i)
					}
				}
				return res
			}
			endDateStr := btime.ToDateStr(endMS, core.DefaultDateFmt)
			startDateStr := btime.ToDateStr(startMS, core.DefaultDateFmt)
			liveLacks := checkLacks(arr)
			localLacks := checkLacks(kline)
			fmt.Printf("%s %s - %s, live: %v, local: %v\n",
				key, startDateStr, endDateStr, liveLacks, localLacks)
		}
		fmt.Printf("total: %v, pairs: %d \n\n", totalNum, len(gpKlines))
	}
	for {
		var rows []*DumpRow
		err = dec.Decode(&rows)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Errorf("Failed to decode dump file: %v", err)
		}
		for _, row := range rows {
			if row == nil {
				continue
			}
			if row.Type != DumpKline {
				printStat()
				dateStr := btime.ToDateStr(row.Time, core.DefaultDateFmt)
				log.Info("receive other", zap.String("src", row.Type), zap.String("date", dateStr))
				gpKlines = make(map[string][]*banexg.Kline)
				endMS = int64(0)
				totalNum = 0
				continue
			}

			kline, ok := row.Val.(banexg.Kline)
			if !ok {
				t.Errorf("Invalid K-line value type for row: %+v", row)
				continue
			}
			totalNum += 1
			endMS = row.Time

			envKeyArr := strings.Split(row.Key, "_")
			symbol, tf := envKeyArr[0], envKeyArr[1]

			tfMSecs := int64(utils.TFToSecs(tf)) * 1000
			timeDiff := (row.Time - kline.Time - tfMSecs) / 60000
			if timeDiff > 1 {
				t.Errorf("K-line time (%d) differs from row time (%d) by more than 2 minutes for symbol %s timeframe %s",
					kline.Time, row.Time, symbol, envKeyArr[1])
				continue
			}

			arr, _ := gpKlines[row.Key]
			gpKlines[row.Key] = append(arr, &kline)
		}
	}
	printStat()
}
