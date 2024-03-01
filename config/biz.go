package config

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v3"
	"maps"
	"os"
	"path/filepath"
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

func GetRunEnv() string {
	runEnv := os.Getenv("BanRunEnv")
	if runEnv == "" {
		return core.RunEnvTest
	} else if runEnv != core.RunEnvTest && runEnv != core.RunEnvProd {
		log.Error(fmt.Sprintf("invalid run env: %v", runEnv))
		return core.RunEnvTest
	}
	return runEnv
}

func LoadConfig(args *CmdArgs) *errs.Error {
	if Loaded {
		return nil
	}
	Args = args
	runEnv := GetRunEnv()
	if runEnv != core.RunEnvProd {
		log.Info("Running in test, Please set `BanRunEnv=prod` in production running")
	}
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
		maps.Copy(merged, unpak)
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
	cutLen := len(Data.TimeRangeRaw) / 2
	Data.TimeRange = &TimeTuple{
		btime.ParseTimeMS(Data.TimeRangeRaw[:cutLen]),
		btime.ParseTimeMS(Data.TimeRangeRaw[cutLen+1:]),
	}
	Name = Data.Name
	core.RunEnv = Data.Env
	core.RunMode = Data.RunMode
	Leverage = Data.Leverage
	if Data.LimitVolSecs == 0 {
		Data.LimitVolSecs = 10
	}
	LimitVolSecs = Data.LimitVolSecs
	core.ExgName = Data.Exchange.Name
	core.Market = Data.MarketType
	if core.Market == banexg.MarketSpot || core.Market == banexg.MarketMargin {
		Data.ContractType = ""
	} else if Data.ContractType == "" {
		Data.ContractType = banexg.MarketSwap
	}
	core.ContractType = Data.ContractType
	MaxMarketRate = Data.MaxMarketRate
	if Data.OdBookTtl == 0 {
		Data.OdBookTtl = 500
	}
	OdBookTtl = Data.OdBookTtl
	OrderType = Data.OrderType
	PreFire = Data.PreFire
	MarginAddRate = Data.MarginAddRate
	ChargeOnBomb = Data.ChargeOnBomb
	AutoEditLimit = Data.AutoEditLimit
	TakeOverStgy = Data.TakeOverStgy
	StakeAmount = Data.StakeAmount
	MinOpenRate = Data.MinOpenRate
	if Data.MaxOpenOrders == 0 {
		Data.MaxOpenOrders = 30
	}
	MaxOpenOrders = Data.MaxOpenOrders
	WalletAmounts = Data.WalletAmounts
	DrawBalanceOver = Data.DrawBalanceOver
	StakeCurrency = Data.StakeCurrency
	FatalStop = Data.FatalStop
	if Data.FatalStopHours == 0 {
		Data.FatalStopHours = 8
	}
	FatalStopHours = Data.FatalStopHours
	TimeRange = Data.TimeRange
	WsStamp = Data.WsStamp
	RunTimeframes = Data.RunTimeframes
	KlineSource = Data.KlineSource
	WatchJobs = Data.WatchJobs
	RunPolicy = Data.RunPolicy
	Pairs = Data.Pairs
	PairMgr = Data.PairMgr
	PairFilters = Data.PairFilters
	Exchange = Data.Exchange
	ExgDataMap = Data.ExgDataMap
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
	for _, item := range core.StgPairTfs {
		if item.Stagy == TakeOverStgy && item.Pair == pair {
			return item.TimeFrame
		}
	}
	return defTF
}
