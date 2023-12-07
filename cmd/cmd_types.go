package cmd

type ArrStringFlag []string

func (i *ArrStringFlag) String() string {
	return "my string representation"
}

func (i *ArrStringFlag) Set(value string) error {
	*i = append(*i, value)
	return nil
}

type CmdArgs struct {
	Configs       ArrStringFlag
	Logfile       string
	DataDir       string
	NoDb          bool
	Debug         bool
	NoCompress    bool
	NoDefault     bool
	TimeRange     string
	RawTimeFrames string
	TimeFrames    []string
	StakeAmount   float64
	RawPairs      string
	Pairs         []string
	Action        string
	RawTables     string
	Tables        []string
	StrategyDirs  ArrStringFlag
	Force         bool
	WithSpider    bool
	Medium        string
	TaskHash      string
	TakId         int
}
