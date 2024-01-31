package orm

var (
	OpenODs    = map[int32]*InOutOrder{} // 全部打开的订单
	UpdateOdMs int64                     // 上次刷新OpenODs的时间戳

)
