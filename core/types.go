package core

type DownRange struct {
	Start   int64
	End     int64
	Reverse bool // 指示是否应该从后往前下载
}

type TfScore struct {
	TF    string
	Score float64
}

type JobPerf struct {
	Num       int
	TotProfit float64 // 总利润
	Score     float64 // 开单倍率，小于1表示减少开单
}

/*
PerfSta 某个策略针对所有标的的统计信息
*/
type PerfSta struct {
	OdNum    int         `yaml:"od_num" mapstructure:"od_num"`
	LastGpAt int         `yaml:"last_gp_at" mapstructure:"last_gp_at"` // 上次执行聚类的订单数量
	Splits   *[4]float64 `yaml:"splits" mapstructure:"splits"`
	Delta    float64     `yaml:"delta" mapstructure:"delta"` // 对TotProfit进行对数处理前的乘数
}

type StrVal struct {
	Str string
	Val float64
}

type Param struct {
	Name  string
	VType int
	Min   float64
	Max   float64
	Mean  float64
	Rate  float64 // 正态分布时有效，默认1，值越大，随机值越趋向于Mean
	edgeY float64 // 计算正态分布边缘y的缓存
}
