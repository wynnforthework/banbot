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

func IsLiveMode(mode string) bool {
	return mode == RunModeProd || mode == RunModeDryRun
}

func LiveMode() bool {
	return IsLiveMode(RunMode)
}

/*
ProdMode
提交到交易所模式
*/
func ProdMode() bool {
	return RunMode == RunModeProd
}

/*
EnvProd 是否使用正式网络
*/
func EnvProd() bool {
	return RunEnv == RunEnvProd
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
