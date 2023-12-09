package products

import (
	"time"
)

type TfScore struct {
	TimeFrame string
	Score     float64
}

// VolumePairListConfig 用于表示按成交量价值倒序排序所有交易对的配置
type VolumePairFilter struct {
	Name          string        `yaml:"name"`
	Limit         int           `yaml:"limit"`          // 返回结果的数量限制，取前100个
	MinValue      float64       `yaml:"min_value"`      // 最低成交量价值
	RefreshSecs   time.Duration `yaml:"refresh_secs"`   // 缓存时间，以秒为单位
	BackTimeframe string        `yaml:"back_timeframe"` // 计算成交量的时间周期，默认为天
	BackPeriod    int           `yaml:"back_period"`    // 与BackTimeframe相乘得到的时间范围的乘数
}

// PriceFilter 价格过滤器配置结构体
type PriceFilter struct {
	Name         string  `yaml:"name"`
	MaxUnitValue float64 `yaml:"max_unit_value"`
	Precision    float64 `yaml:"precision"`
	MinPrice     float64 `yaml:"min_price"`
	MaxPrice     float64 `yaml:"max_price"`
}

// RangeStabilityFilter 结构体用于表示波动性过滤器的配置信息
type RangeStabilityFilter struct {
	Name          string  `yaml:"name"`
	BackDays      int     `yaml:"back_days"`      // 回顾的K线天数
	MinChgRate    float64 `yaml:"min_chg_rate"`   // 最小价格变动比率
	MaxChgRate    float64 `yaml:"max_chg_rate"`   // 最大价格变动比率
	RefreshPeriod int     `yaml:"refresh_period"` // 缓存时间，秒
}

// 流动性过滤器。
type SpreadFilter struct {
	Name     string  `yaml:"name"`
	MaxRatio float32 `yaml:"max_ratio"` // 公式：1-bid/ask，买卖价差占价格的最大比率
}

// VolatilityFilter 表示波动性过滤器的配置
type VolatilityFilter struct {
	Name          string  `yaml:"name"`
	BackDays      int     `yaml:"back_days"`      // 回顾的K线天数
	Max           float64 `yaml:"max"`            // 波动分数最大值
	Min           float64 `yaml:"min"`            // 波动分数最小值
	RefreshPeriod int     `yaml:"refresh_period"` // 缓存时间
}

type AgeFilter struct {
	Name string `yaml:"name"`
	Min  int    `yaml:"min"` // 最小上市天数
	Max  int    `yaml:"max"` // 最大上市天数
}

type OffsetFilter struct {
	Name string `yaml:"name"`
	// 偏移限定数量选择。一般用在最后
	Offset int `yaml:"offset"`
	// 从第10个开始取
	Limit int `yaml:"limit"`
}

type ShufflePairFilter struct {
	Name string `yaml:"name"`
	Seed int    `yaml:"seed"`
}
