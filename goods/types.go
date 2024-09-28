package goods

import (
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
)

type IFilter interface {
	GetName() string
	IsDisable() bool
	IsNeedTickers() bool
	Filter(pairs []string, tickers map[string]*banexg.Ticker) ([]string, *errs.Error)
}

type IProducer interface {
	IFilter
	GenSymbols(tickers map[string]*banexg.Ticker) ([]string, *errs.Error)
}

type BaseFilter struct {
	Name        string `yaml:"name" mapstructure:"name"`
	Disable     bool   `yaml:"disable" mapstructure:"disable"`
	NeedTickers bool
}

// VolumePairFilter Used to represent a configuration that sorts all trading pairs in reverse order by volume value 用于表示按成交量价值倒序排序所有交易对的配置
type VolumePairFilter struct {
	BaseFilter
	Limit         int     `yaml:"limit" mapstructure:"limit,omitempty"`                   // The number of returned results is limited to the first 100 返回结果的数量限制，取前100个
	MinValue      float64 `yaml:"min_value" mapstructure:"min_value,omitempty"`           // Minimum volume value 最低成交量价值
	RefreshSecs   int     `yaml:"refresh_secs" mapstructure:"refresh_secs,omitempty"`     // Cache time, in seconds 缓存时间，以秒为单位
	BackTimeframe string  `yaml:"back_timeframe" mapstructure:"back_timeframe,omitempty"` // The time period for calculating the volume, which is set to days by default 计算成交量的时间周期，默认为天
	BackPeriod    int     `yaml:"back_period" mapstructure:"back_period,omitempty"`       // The multiplier for the time range obtained by multiplying it by the BackTimeframe 与BackTimeframe相乘得到的时间范围的乘数
}

/*
PriceFilter The price filter configuration 价格过滤器配置结构体
Precision: 0.001，Filter trading pairs by price precision, and the minimum unit of price change is 0.1% by default 按价格精度过滤交易对，默认要求价格变动最小单位是0.1%
Min: Lowest price 最低价格
Max: Highest price 最高价格
MaxUnitValue: The value of the maximum allowable unit price change (for the pricing currency, it is generally USDT). 最大允许的单位价格变动对应的价值(针对定价货币，一般是USDT)。
*/
type PriceFilter struct {
	BaseFilter
	MaxUnitValue float64 `yaml:"max_unit_value" mapstructure:"max_unit_value,omitempty"`
	Precision    float64 `yaml:"precision" mapstructure:"precision,omitempty"`
	Min          float64 `yaml:"min" mapstructure:"min,omitempty"`
	Max          float64 `yaml:"max" mapstructure:"max,omitempty"`
}

// RateOfChangeFilter 一段时间内(high-low)/low比值
type RateOfChangeFilter struct {
	BaseFilter
	BackDays      int     `yaml:"back_days" mapstructure:"back_days,omitempty"`           // 回顾的K线天数
	Min           float64 `yaml:"min" mapstructure:"min,omitempty"`                       // 最小价格变动比率
	Max           float64 `yaml:"max" mapstructure:"max,omitempty"`                       // 最大价格变动比率
	RefreshPeriod int     `yaml:"refresh_period" mapstructure:"refresh_period,omitempty"` // 缓存时间，秒
}

// 流动性过滤器。
type SpreadFilter struct {
	BaseFilter
	MaxRatio float32 `yaml:"max_ratio" mapstructure:"max_ratio,omitempty"` // 公式：1-bid/ask，买卖价差占价格的最大比率
}

type CorrelationFilter struct {
	BaseFilter
	Min       float64 `yaml:"min" mapstructure:"min,omitempty"`
	Max       float64 `yaml:"max" mapstructure:"max,omitempty"`
	Timeframe string  `yaml:"timeframe" mapstructure:"timeframe,omitempty"`
	BackNum   int     `yaml:"back_num" mapstructure:"back_num,omitempty"`
	TopN      int     `yaml:"top_n" mapstructure:"top_n"`
}

// VolatilityFilter StdDev(ln(close / prev_close)) * sqrt(num)
type VolatilityFilter struct {
	BaseFilter
	BackDays      int     `yaml:"back_days" mapstructure:"back_days,omitempty"`           // 回顾的K线天数
	Max           float64 `yaml:"max" mapstructure:"max,omitempty"`                       // 波动分数最大值
	Min           float64 `yaml:"min" mapstructure:"min,omitempty"`                       // 波动分数最小值
	RefreshPeriod int     `yaml:"refresh_period" mapstructure:"refresh_period,omitempty"` // 缓存时间
}

type AgeFilter struct {
	BaseFilter
	Min int `yaml:"min" mapstructure:"min,omitempty"` // 最小上市天数
	Max int `yaml:"max" mapstructure:"max,omitempty"` // 最大上市天数
}

type OffsetFilter struct {
	BaseFilter
	// 偏移限定数量选择。一般用在最后
	Reverse bool `yaml:"reverse" mapstructure:"reverse,omitempty"`
	// 偏移限定数量选择。一般用在最后
	Offset int `yaml:"offset" mapstructure:"offset,omitempty"`
	// 从第10个开始取
	Limit int `yaml:"limit" mapstructure:"limit,omitempty"`
}

type ShuffleFilter struct {
	BaseFilter
	Seed int `yaml:"seed" mapstructure:"seed,omitempty"`
}
