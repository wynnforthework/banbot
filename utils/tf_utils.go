package utils

import (
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/utils"
)

func init() {
	utils.RegTfSecs(map[string]int{
		"ws": 5,
	})
}

/*
BuildOHLCV
Build or update coarser grained OHLC arrays from transactions or sub OHLC arrays.
Arr: List of sub OHLC.
ToTFSecs: Specify the time granularity to be built, in milliseconds
PreFire: The rate at which builds are triggered ahead of schedule;
ResOHLCV: Existing array to be updated
From TFSets: The interval between the arr subarrays passed in, calculated when not provided, in milliseconds
OffMS: offset of alignment time
从交易或子OHLC数组中，构建或更新更粗粒度OHLC数组。
arr: 子OHLC列表。
toTFSecs: 指定要构建的时间粒度，单位：毫秒
preFire: 提前触发构建完成的比率；
resOHLCV: 已有的待更新数组
fromTFSecs: 传入的arr子数组间隔，未提供时计算，单位：毫秒
offMS: 对齐时间的偏移
*/
func BuildOHLCV(arr []*banexg.Kline, toTFMSecs int64, preFire float64, resOHLCV []*banexg.Kline, fromTFMS, offMS int64) ([]*banexg.Kline, bool) {
	_, offset := utils.GetTfAlignOrigin(int(toTFMSecs / 1000))
	alignOffMS := int64(offset*1000) + offMS
	offsetMS := int64(float64(toTFMSecs) * preFire)
	subNum := len(arr)
	if fromTFMS == 0 && subNum >= 2 {
		fromTFMS = arr[subNum-1].Time - arr[subNum-2].Time
	}
	var big *banexg.Kline
	aggNum, cacheNum := 0, 0
	if fromTFMS > 0 {
		aggNum = int(toTFMSecs / fromTFMS) // 大周期由几个小周期组成
		cacheNum = len(arr)/aggNum + 3
	}
	if resOHLCV == nil {
		resOHLCV = make([]*banexg.Kline, 0, cacheNum)
	} else if len(resOHLCV) > 0 {
		cutLen := len(resOHLCV) - 1
		big = resOHLCV[cutLen]
		resOHLCV = resOHLCV[:cutLen]
	}
	aggCnt := 0 // 当前大周期bar从小周期聚合的数量
	for _, bar := range arr {
		timeAlign := utils.AlignTfMSecsOffset(bar.Time+offsetMS, toTFMSecs, alignOffMS)
		if big != nil && big.Time == timeAlign {
			// 属于同一个
			if bar.Volume > 0 {
				if big.Volume == 0 {
					big.Open = bar.Open
					big.High = bar.High
					big.Low = bar.Low
				} else {
					if bar.High > big.High {
						big.High = bar.High
					}
					if bar.Low < big.Low {
						big.Low = bar.Low
					}
				}
				big.Close = bar.Close
				big.Volume += bar.Volume
				big.Info = bar.Info
			}
			aggCnt += 1
		} else {
			if aggCnt > aggNum {
				aggNum = aggCnt
			}
			if big != nil {
				if big.Volume > 0 || aggCnt*5 > aggNum {
					// 跳过小周期数量不足20%，且总成交量为0的
					resOHLCV = append(resOHLCV, big)
				}
			}
			big = bar.Clone() // 不修改原始数据
			big.Time = timeAlign
			aggCnt = 1
		}
	}
	if big != nil && big.Volume > 0 || aggCnt*5 > aggNum {
		// 跳过小周期数量不足20%，且总成交量为0的
		resOHLCV = append(resOHLCV, big)
	}
	lastFinished := false
	if fromTFMS > 0 && len(resOHLCV) > 0 {
		// 判断最后一个bar是否结束：假定arr中每个bar间隔相等，最后一个bar+间隔属于下一个规划区间，则认为最后一个bar结束
		finishMS := utils.AlignTfMSecsOffset(arr[subNum-1].Time+fromTFMS+offsetMS, toTFMSecs, alignOffMS)
		lastFinished = finishMS > resOHLCV[len(resOHLCV)-1].Time
	}
	return resOHLCV, lastFinished
}
