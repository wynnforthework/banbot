package live

import (
	"fmt"
	"github.com/anyongjin/cron"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banexg/bntp"
	"github.com/banbox/banexg/log"
	"log/slog"
	"testing"
	"time"
)

func TestCron(t *testing.T) {
	bntp.LangCode = "zh-CN"
	cron.FnTimeNow = func() time.Time {
		return *btime.Now()
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)
	c := cron.New(cron.WithSeconds(), cron.WithLogger(slog.Default()))
	c.Add("0 * * * * *", func() {
		realTime := btime.UTCStamp()      // correct utc timestamp
		sysTime := time.Now().UnixMilli() // local system timestamp
		log.Info(fmt.Sprintf("system: %d, real: %d", sysTime, realTime))
	})
	c.Start()
	time.Sleep(time.Minute * 3)
}
