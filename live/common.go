package live

import (
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/opt"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/rpc"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"slices"
	"strconv"
)

var (
	lastNotifyDelay = int64(0)
	lastRefreshMS   = int64(0)
)

func CronRefreshPairs(dp data.IProvider) {
	if config.PairMgr.Cron != "" {
		_, err_ := core.Cron.AddFunc(config.PairMgr.Cron, func() {
			curMS := btime.TimeMS()
			if curMS-lastRefreshMS < config.MinPairCronGapMS {
				return
			}
			lastRefreshMS = curMS
			err := opt.RefreshPairJobs(dp, true, false, nil)
			if err != nil {
				log.Error("RefreshPairJobs fail", zap.Error(err))
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
		_, _ = orm.LoadMarkets(exg.Default, true)
	})
	if err != nil {
		log.Error("add CronLoadMarkets fail", zap.Error(err))
	}
}

func CronFatalLossCheck() {
	checkIntvs := utils.KeysOfMap(config.FatalStop)
	if len(checkIntvs) == 0 {
		return
	}
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
			msgText := "Listen to the spider kline timeout:" + failText
			log.Warn(msgText)
			if curMS-lastNotifyDelay > 600000 {
				// Delay reminders are sent every 10 minutes
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
		var pairGroups = make(map[string][]string)
		for tf, tfMap := range core.TfPairHits {
			hitMap := make(map[int][]string)
			for pair, num := range tfMap {
				arr, _ := hitMap[num]
				hitMap[num] = append(arr, pair)
			}
			for num, arr := range hitMap {
				arrLen := len(arr)
				pairGroups[fmt.Sprintf("%s_%v: %v", tf, num, arrLen)] = arr
			}
			core.TfPairHits[tf] = make(map[string]int)
		}
		if len(pairGroups) > 0 {
			staText := core.GroupByPairQuotes(pairGroups)
			log.Info(fmt.Sprintf("receive bars in 5 mins:\n%s", staText))
		}
	})
	if err_ != nil {
		log.Error("add Receive Klines Summary fail", zap.Error(err_))
	}
}

func CronCheckTriggerOds() {
	// Check every minute 15 seconds to see if the limit order submission is triggered
	// 在每分钟的15s检查是否触发限价单提交
	_, err_ := core.Cron.AddFunc("15 * * * * *", biz.VerifyTriggerOds)
	if err_ != nil {
		log.Error("add VerifyTriggerOds fail", zap.Error(err_))
	}
}
