package goods

import (
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
)

type IFilter interface {
	GetName() string
	IsEnable() bool
	IsNeedTickers() bool
	Filter(pairs []string, tickers map[string]*banexg.Ticker) ([]string, *errs.Error)
}

type IProducer interface {
	IFilter
	GenSymbols(tickers map[string]*banexg.Ticker) ([]string, *errs.Error)
}

type BaseFilter struct {
	Name        string `yaml:"name" mapstructure:"name"`
	Enable      bool   `yaml:"enable" mapstructure:"enable"`
	NeedTickers bool
}

// VolumePairFilter 用于表示按成交量价值倒序排序所有交易对的配置
type VolumePairFilter struct {
	BaseFilter
	Limit         int     `yaml:"limit" mapstructure:"limit,omitempty"`                   // 返回结果的数量限制，取前100个
	MinValue      float64 `yaml:"min_value" mapstructure:"min_value,omitempty"`           // 最低成交量价值
	RefreshSecs   int     `yaml:"refresh_secs" mapstructure:"refresh_secs,omitempty"`     // 缓存时间，以秒为单位
	BackTimeframe string  `yaml:"back_timeframe" mapstructure:"back_timeframe,omitempty"` // 计算成交量的时间周期，默认为天
	BackPeriod    int     `yaml:"back_period" mapstructure:"back_period,omitempty"`       // 与BackTimeframe相乘得到的时间范围的乘数
}

/*
PriceFilter 价格过滤器配置结构体
Precision: 0.001，按价格精度过滤交易对，默认要求价格变动最小单位是0.1%
Min: 最低价格
Max: 最高价格
MaxUnitValue: 最大允许的单位价格变动对应的价值(针对定价货币，一般是USDT)。
*/
type PriceFilter struct {
	BaseFilter
	MaxUnitValue float64 `yaml:"max_unit_value" mapstructure:"max_unit_value,omitempty"`
	Precision    float64 `yaml:"precision" mapstructure:"precision,omitempty"`
	Min          float64 `yaml:"min" mapstructure:"min,omitempty"`
	Max          float64 `yaml:"max" mapstructure:"max,omitempty"`
}

// RateOfChangeFilter 结构体用于表示波动性过滤器的配置信息
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

// VolatilityFilter 表示波动性过滤器的配置
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
	Offset int `yaml:"offset" mapstructure:"offset,omitempty"`
	// 从第10个开始取
	Limit int `yaml:"limit" mapstructure:"limit,omitempty"`
}

type ShuffleFilter struct {
	BaseFilter
	Seed int `yaml:"seed" mapstructure:"seed,omitempty"`
}
