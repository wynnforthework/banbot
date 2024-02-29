package live

import (
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/rpc"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"strings"
)

func CronRefreshPairs() {
	if config.PairMgr != nil && config.PairMgr.Cron != "" {
		_, err_ := core.Cron.AddFunc(config.PairMgr.Cron, func() {
			err := goods.RefreshPairList(nil)
			if err != nil {
				log.Error("refresh pairs fail", zap.Error(err))
			}
		})
		if err_ != nil {
			log.Error("add RefreshPairList fail", zap.Error(err_))
		}
	}
}

func CronLoadMarkets() {
	// 2小时更新一次市场行情
	_, err := core.Cron.AddFunc("30 3 */2 * * *", func() {
		_, _ = exg.Default.LoadMarkets(true, nil)
	})
	if err != nil {
		log.Error("add CronLoadMarkets fail", zap.Error(err))
	}
}

func CronFatalLossCheck() {
	_, err := core.Cron.AddFunc("35 */5 * * * *", biz.CheckFatalStop)
	if err != nil {
		log.Error("add CronFatalLossCheck fail", zap.Error(err))
	}
}

var lastNotifyDelay = int64(0)

func CronKlineDelays() {
	_, err_ := core.Cron.AddFunc("30 * * * * *", func() {
		curMS := btime.TimeMS()
		var fails []string
		for pair, wait := range core.PairCopiedMs {
			if wait[0]+wait[1]*2 > curMS {
				continue
			}
			timeoutMin := (curMS - wait[0]) / 60000
			fails = append(fails, fmt.Sprintf("%s: %v", pair, timeoutMin))
		}
		if len(fails) > 0 {
			failText := strings.Join(fails, ", ")
			msgText := "监听爬虫K线超时：" + failText
			log.Error(msgText)
			if curMS-lastNotifyDelay > 600000 {
				// 10 分钟发送一次延迟提醒
				lastNotifyDelay = curMS
				rpc.SendMsg(map[string]interface{}{
					"type":   rpc.MsgTypeException,
					"status": msgText,
				})
			}
		}
	})
	if err_ != nil {
		log.Error("add Monitor Klines fail", zap.Error(err_))
	}
}

func CronKlineSummary() {
	_, err_ := core.Cron.AddFunc("30 1-59/5 * * * *", func() {
		var res = make(map[string]string)
		for tf, tfMap := range core.TfPairHits {
			hitMap := make(map[int][]string)
			for pair, num := range tfMap {
				arr, _ := hitMap[num]
				hitMap[num] = append(arr, pair)
			}
			for num, arr := range hitMap {
				res[fmt.Sprintf("%s_%v", tf, num)] = strings.Join(arr, ", ")
			}
			core.TfPairHits[tf] = make(map[string]int)
		}
		if len(res) > 0 {
			var b strings.Builder
			for key, text := range res {
				b.WriteString("[")
				b.WriteString(key)
				b.WriteString("] ")
				b.WriteString(text)
				b.WriteString("\n")
			}
			log.Info(fmt.Sprintf("receive bars in 5 mins:\n%s", b.String()))
		}
	})
	if err_ != nil {
		log.Error("add Receive Klines Summary fail", zap.Error(err_))
	}
}
