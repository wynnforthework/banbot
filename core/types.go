package core

type StgPairTf struct {
	Stagy     string
	Pair      string
	TimeFrame string
}

type DownRange struct {
	Start   int64
	End     int64
	Reverse bool // 指示是否应该从后往前下载
}

type TfScore struct {
	TF    string
	Score float64
}

type TFSecTuple struct {
	TF   string
	Secs int
}
