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
	inPath := "E:\\trade\\go\\bandata\\dump\\4h3.gob"
	file, err := os.Open(inPath)
	if err != nil {
		t.Fatalf("Failed to open dump file: %v", err)
	}
	defer file.Close()

	gob.Register(banexg.Kline{})
	gob.Register([]*DumpRow{})
	dec := gob.NewDecoder(file)

	lastKline := make(map[string]*banexg.Kline)
	reachLive := false // 头部有些预热的K线需过滤，检查是否预热完成
	prevMS := int64(0)
	delayMS := int64(3000) //两个记录间隔低于此视为预热
	checkNum, totalNum := 0, 0
	printStat := func() {
		msg := fmt.Sprintf("total: %v check: %v", totalNum, checkNum)
		log.Info(msg)
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
				lastKline = make(map[string]*banexg.Kline)
				prevMS = int64(0)
				reachLive = false
				checkNum, totalNum = 0, 0
				continue
			}

			kline, ok := row.Val.(banexg.Kline)
			if !ok {
				t.Errorf("Invalid K-line value type for row: %+v", row)
				continue
			}
			totalNum += 1
			if !reachLive && (prevMS == 0 || row.Time-prevMS < delayMS) {
				prevMS = row.Time
				continue
			}

			envKeyArr := strings.Split(row.Key, "_")
			symbol, tf := envKeyArr[0], envKeyArr[1]

			dateStr := btime.ToDateStr(kline.Time, core.DefaultDateFmt)
			fmt.Printf("%s %s %s %.6f %.6f\n", dateStr, tf, symbol, kline.Close, kline.Volume)

			tfMSecs := int64(utils.TFToSecs(tf)) * 1000
			timeDiff := (row.Time - kline.Time) / 60000
			if timeDiff > 1 {
				if reachLive {
					t.Errorf("K-line time (%d) differs from row time (%d) by more than 2 minutes for symbol %s timeframe %s",
						kline.Time, row.Time, symbol, envKeyArr[1])
				}
				continue
			} else if !reachLive {
				reachLive = true
			}
			checkNum += 1

			if prev, ok := lastKline[row.Key]; ok {
				interval := kline.Time - prev.Time
				if interval != tfMSecs {
					t.Errorf("K-line interval mismatch for %s: expected %dms, got %dms. Missing K-lines between %d and %d",
						row.Key, tfMSecs, interval, prev.Time, kline.Time)
				}
			}
			lastKline[row.Key] = &kline
		}
	}
	printStat()
}
