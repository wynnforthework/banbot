package core

import "time"

func GetStagyJobs(stagy string) map[string]string {
	var result = make(map[string]string)
	for _, item := range StgPairTfs {
		if item.Stagy == stagy {
			result[item.Pair] = item.TimeFrame
		}
	}
	return result
}

func SetRunMode(mode string) {
	RunMode = mode
	LiveMode = RunMode == RunModeLive
}

func SetRunEnv(env string) {
	RunEnv = env
	EnvReal = RunEnv != RunEnvDryRun
}

/*
SetPairMs
更新bot端从爬虫收到的标的最新时间和等待间隔
*/
func SetPairMs(pair string, barMS, waitMS int64) {
	PairCopiedMs[pair] = [2]int64{barMS, waitMS}
	LastBarMs = max(LastBarMs, barMS)
}

func Sleep(d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-Ctx.Done():
		return false
	}
}
