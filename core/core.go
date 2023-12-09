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
