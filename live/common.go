package live

import (
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/rpc"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"slices"
	"strconv"
	"strings"
)

func CronRefreshPairs() {
	if config.PairMgr != nil && config.PairMgr.Cron != "" {
		_, err_ := core.Cron.AddFunc(config.PairMgr.Cron, biz.AutoRefreshPairs)
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
	checkIntvs := utils.KeysOfMap(config.FatalStop)
	minIntv := slices.Min(checkIntvs)
	if minIntv < 1 {
		log.Error("fatal_stop invalid, min is 1, skip", zap.Int("current", minIntv))
		return
	}
	cronStr := fmt.Sprintf("35 */%v * * * *", min(5, minIntv))
	maxIntv := slices.Max(checkIntvs)
	_, err := core.Cron.AddFunc(cronStr, biz.MakeCheckFatalStop(maxIntv))
	if err != nil {
		log.Error("add CronFatalLossCheck fail", zap.Error(err))
	}
}

var lastNotifyDelay = int64(0)

func CronKlineDelays() {
	_, err_ := core.Cron.AddFunc("30 * * * * *", func() {
		curMS := btime.TimeMS()
		var fails = make(map[string][]string)
		for pair, wait := range core.PairCopiedMs {
			if wait[0]+wait[1]*2 > curMS {
				continue
			}
			timeoutMin := strconv.Itoa(int((curMS - wait[0]) / 60000))
			arr, _ := fails[timeoutMin]
			fails[timeoutMin] = append(arr, pair)
		}
		if len(fails) > 0 {
			failText := core.GroupByPairQuotes(fails)
			msgText := "监听爬虫K线超时：" + failText
			log.Warn(msgText)
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
				arrLen := len(arr)
				res[fmt.Sprintf("%s_%v: %v", tf, num, arrLen)] = strings.Join(arr, ", ")
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

func CronCheckTriggerOds() {
	// 在每分钟的15s检查是否触发限价单提交
	_, err_ := core.Cron.AddFunc("15 * * * * *", biz.VerifyTriggerOds)
	if err_ != nil {
		log.Error("add VerifyTriggerOds fail", zap.Error(err_))
	}
}

func CronCancelOldLimits() {
	// 在每分钟的10s检查是否触发限价单提交
	_, err_ := core.Cron.AddFunc("10 * * * * *", biz.CancelOldLimits)
	if err_ != nil {
		log.Error("add CancelOldLimits fail", zap.Error(err_))
	}
}
