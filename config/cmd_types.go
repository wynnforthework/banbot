package config

type ArrString []string

func (i *ArrString) String() string {
	return "my string representation"
}

func (i *ArrString) Set(value string) error {
	*i = append(*i, value)
	return nil
}

type CmdArgs struct {
	Configs       ArrString
	Logfile       string
	DataDir       string
	NoDb          bool
	NoCompress    bool
	NoDefault     bool
	LogLevel      string
	TimeRange     string
	RawTimeFrames string
	TimeFrames    []string
	StakeAmount   float64
	StakePct      float64
	RawPairs      string
	Pairs         []string
	RawTables     string
	Tables        []string
	StrategyDirs  ArrString
	Force         bool
	WithSpider    bool
	Medium        string
	TaskHash      string
	TaskId        int
	MaxPoolSize   int
	CPUProfile    bool
	MemProfile    bool
	InPath        string
	OutPath       string
	AdjType       string // 复权类型: pre,post,none
	TimeZone      string // 时区
	ExgReal       string
	OptRounds     int    // 超参数优化单任务执行轮次
	Concur        int    // 超参数优化多进程并发数量
	Sampler       string // 超参数优化的方法: tpe/bayes/random/cmaes/ipop-cmaes/bipop-cmaes
	EachPairs     bool   // 逐个标的执行
	ReviewPeriod  string // 持续调参回测时，调参回顾的周期
	RunPeriod     string // 持续调参回测时，调参后有效运行周期
	InType        string // 输入文件的数据类型
}
