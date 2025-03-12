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
	select {
	case <-time.After(d):
		return true
	case <-Ctx.Done():
		return false
	}
}
