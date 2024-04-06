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
	Debug         bool
	NoCompress    bool
	NoDefault     bool
	FixTFKline    bool
	TimeRange     string
	RawTimeFrames string
	TimeFrames    []string
	StakeAmount   float64
	StakePct      float64
	RawPairs      string
	Pairs         []string
	Action        string
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
}
