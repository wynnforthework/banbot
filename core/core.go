package core

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
