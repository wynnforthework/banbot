package config

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	utils2 "github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

func GetDataDir() string {
	if DataDir == "" {
		DataDir = os.Getenv("BanDataDir")
	}
	return DataDir
}

func GetStagyDir() string {
	result := os.Getenv("BanStgyDir")
	if result == "" {
		panic(fmt.Errorf("`BanStgyDir` env is required"))
	}
	return result
}

func LoadConfig(args *CmdArgs) *errs.Error {
	if Loaded {
		return nil
	}
	Args = args
	DataDir = args.DataDir
	var paths []string
	if !args.NoDefault {
		dataDir := GetDataDir()
		if dataDir == "" {
			return errs.NewMsg(errs.CodeParamRequired, "data_dir is required")
		}
		tryNames := []string{"config.yml", "config.local.yml"}
		for _, name := range tryNames {
			path := filepath.Join(dataDir, name)
			if _, err := os.Stat(path); err == nil {
				paths = append(paths, path)
			}
		}
	}
	if args.Configs != nil {
		paths = append(paths, args.Configs...)
	}
	var merged = make(map[string]interface{})
	for _, path := range paths {
		log.Info("Using " + path)
		fileData, err := os.ReadFile(path)
		if err != nil {
			return errs.NewFull(core.ErrIOReadFail, err, "Read %s Fail", path)
		}
		var unpak map[string]interface{}
		err = yaml.Unmarshal(fileData, &unpak)
		if err != nil {
			return errs.NewFull(errs.CodeUnmarshalFail, err, "Unmarshal %s Fail", path)
		}
		utils2.DeepCopyMap(merged, unpak)
	}
	err := mapstructure.Decode(merged, &Data)
	if err != nil {
		return errs.NewFull(errs.CodeUnmarshalFail, err, "decode Config Fail")
	}
	return apply(args)
}

func apply(args *CmdArgs) *errs.Error {
	Loaded = true
	Debug = args.Debug
	NoDB = args.NoDb
	if args.TimeRange != "" {
		Data.TimeRangeRaw = args.TimeRange
	}
	if args.MaxPoolSize > 0 {
		Data.Database.MaxPoolSize = args.MaxPoolSize
	}
	cutLen := len(Data.TimeRangeRaw) / 2
	Data.TimeRange = &TimeTuple{
		btime.ParseTimeMS(Data.TimeRangeRaw[:cutLen]),
		btime.ParseTimeMS(Data.TimeRangeRaw[cutLen+1:]),
	}
	Name = Data.Name
	ReClientID = regexp.MustCompile(fmt.Sprintf("^%s_(\\d+)(_\\d+)?$", Name))
	core.SetRunEnv(Data.Env)
	Leverage = Data.Leverage
	if Data.LimitVolSecs == 0 {
		Data.LimitVolSecs = 10
	}
	LimitVolSecs = Data.LimitVolSecs
	if Data.PutLimitSecs == 0 {
		Data.PutLimitSecs = 120
	}
	PutLimitSecs = Data.PutLimitSecs
	core.ExgName = Data.Exchange.Name
	core.Market = Data.MarketType
	if core.Market == banexg.MarketSpot || core.Market == banexg.MarketMargin {
		Data.ContractType = ""
	} else if Data.ContractType == "" {
		Data.ContractType = banexg.MarketSwap
	}
	core.ContractType = Data.ContractType
	if Data.OdBookTtl == 0 {
		Data.OdBookTtl = 500
	}
	OdBookTtl = Data.OdBookTtl
	StopEnterBars = Data.StopEnterBars
	if Data.ConcurNum == 0 {
		Data.ConcurNum = 2
	}
	core.ConcurNum = Data.ConcurNum
	OrderType = Data.OrderType
	PreFire = Data.PreFire
	if Data.MarginAddRate == 0 {
		Data.MarginAddRate = 0.66
	}
	MarginAddRate = Data.MarginAddRate
	ChargeOnBomb = Data.ChargeOnBomb
	TakeOverStgy = Data.TakeOverStgy
	if args.StakeAmount > 0 {
		Data.StakeAmount = args.StakeAmount
	}
	StakeAmount = Data.StakeAmount
	if args.StakePct > 0 {
		Data.StakePct = args.StakePct
	}
	StakePct = Data.StakePct
	MaxStakeAmt = Data.MaxStakeAmt
	if Data.OpenVolRate == 0 {
		Data.OpenVolRate = 1
	}
	OpenVolRate = Data.OpenVolRate
	if Data.MinOpenRate == 0 {
		Data.MinOpenRate = 0.5
	}
	MinOpenRate = Data.MinOpenRate
	if Data.BTNetCost == 0 {
		Data.BTNetCost = 15
	}
	BTNetCost = Data.BTNetCost
	if Data.MaxOpenOrders == 0 {
		Data.MaxOpenOrders = 30
	}
	MaxOpenOrders = Data.MaxOpenOrders
	WalletAmounts = Data.WalletAmounts
	DrawBalanceOver = Data.DrawBalanceOver
	StakeCurrency = Data.StakeCurrency
	if len(StakeCurrency) == 0 {
		panic("config `stake_currency` cannot be empty")
	}
	StakeCurrencyMap = make(map[string]bool)
	for _, curr := range Data.StakeCurrency {
		StakeCurrencyMap[curr] = true
	}
	FatalStop = make(map[int]float64)
	if len(Data.FatalStop) > 0 {
		for text, rate := range Data.FatalStop {
			mins, err_ := strconv.Atoi(text)
			if err_ != nil || mins < 1 {
				panic("config fatal_stop." + text + " invalid, must be int >= 1")
			}
			FatalStop[mins] = rate
		}
	}
	if Data.FatalStopHours == 0 {
		Data.FatalStopHours = 8
	}
	FatalStopHours = Data.FatalStopHours
	TimeRange = Data.TimeRange
	if len(args.TimeFrames) > 0 {
		Data.RunTimeframes = args.TimeFrames
	}
	RunTimeframes = Data.RunTimeframes
	WatchJobs = Data.WatchJobs
	if Data.RunPolicy == nil {
		Data.RunPolicy = make(map[string]*RunPolicyConfig)
	}
	for name, pol := range Data.RunPolicy {
		pol.Name = name
	}
	RunPolicy = Data.RunPolicy
	if len(args.Pairs) > 0 {
		Data.Pairs = args.Pairs
	}
	Pairs = Data.Pairs
	if Data.PairMgr == nil {
		Data.PairMgr = &PairMgrConfig{}
	}
	PairMgr = Data.PairMgr
	PairFilters = Data.PairFilters
	Exchange = Data.Exchange
	initExgAccs()
	Database = Data.Database
	SpiderAddr = Data.SpiderAddr
	APIServer = Data.APIServer
	RPCChannels = Data.RPCChannels
	Webhook = Data.Webhook
	return nil
}

func GetExgConfig() *ExgItemConfig {
	if cfg, ok := Exchange.Items[Exchange.Name]; ok {
		return cfg
	}
	return &ExgItemConfig{}
}

func GetTakeOverTF(pair, defTF string) string {
	if pairMap, ok := core.StgPairTfs[TakeOverStgy]; ok {
		if tf, ok := pairMap[pair]; ok {
			return tf
		}
	}
	return defTF
}

func GetAccLeverage(account string) int {
	acc, ok := Accounts[account]
	if ok && acc.Leverage > 0 {
		return acc.Leverage
	} else {
		return Leverage
	}
}

func initExgAccs() {
	exgCfg := GetExgConfig()
	var accs map[string]*AccountConfig
	if core.RunEnv != core.RunEnvTest {
		accs = exgCfg.AccountProds
	} else {
		accs = exgCfg.AccountTests
	}
	Accounts = make(map[string]*AccountConfig)
	BakAccounts = make(map[string]*AccountConfig)
	if len(accs) == 0 {
		return
	}
	for name, val := range accs {
		if val.NoTrade {
			BakAccounts[name] = val
		} else if !core.EnvReal {
			// 非生产环境，只启用一个账号
			Accounts[DefAcc] = val
		} else {
			Accounts[name] = val
		}
	}
}
