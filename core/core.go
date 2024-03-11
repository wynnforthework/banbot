package core

import (
	"time"
)

func SetRunMode(mode string) {
	RunMode = mode
	LiveMode = RunMode == RunModeLive
}

func SetRunEnv(env string) {
	RunEnv = env
	if LiveMode {
		EnvReal = RunEnv != RunEnvDryRun
	} else {
		EnvReal = false
	}
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
