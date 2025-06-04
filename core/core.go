package core

import (
	"time"
)

func SetRunMode(mode string) {
	RunMode = mode
	LiveMode = RunMode == RunModeLive
	BackTestMode = RunMode == RunModeBackTest
}

func SetRunEnv(env string) {
	RunEnv = env
	if LiveMode {
		EnvReal = RunEnv != RunEnvDryRun
	} else {
		EnvReal = false
	}
}

func Sleep(d time.Duration) bool {
	if Ctx == nil {
		time.Sleep(d)
		return true
	}
	select {
	case <-time.After(d):
		return true
	case <-Ctx.Done():
		return false
	}
}
