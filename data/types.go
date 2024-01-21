package data

import "github.com/banbox/banexg"

type PairTFCache struct {
	TimeFrame string
	TFSecs    int
	NextMS    int64         // 下一个需要的13位时间戳。一般和WaitBar不应该同时使用
	WaitBar   *banexg.Kline // 记录尚未完成的bar。已完成时应置为nil
	Latest    *banexg.Kline // 记录最新bar数据，可能未完成，可能已完成
}
