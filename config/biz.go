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
	return os.Getenv("BanDataDir")
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
		data, err := os.ReadFile(path)
		if err != nil {
			return errs.NewMsg(core.ErrIOReadFail, "Read %s Fail: %v", path, err)
		}
		var unpak map[string]interface{}
		err = yaml.Unmarshal(data, &unpak)
		if err != nil {
			return errs.NewMsg(errs.CodeUnmarshalFail, "Unmarshal %s Fail: %v", path, err)
		}
		maps.Copy(merged, unpak)
	}
	err := mapstructure.Decode(merged, &data)
	if err != nil {
		return errs.NewMsg(errs.CodeUnmarshalFail, "decode Config Fail: %v", err)
	}
	return apply(args)
}

func apply(args *CmdArgs) *errs.Error {
	Loaded = true
	Debug = args.Debug
	NoDB = args.NoDb
	if args.TimeRange != "" {
		data.TimeRangeRaw = args.TimeRange
	}
	cutLen := len(data.TimeRangeRaw) / 2
	data.TimeRange = &TimeTuple{
		btime.ParseTimeMS(data.TimeRangeRaw[:cutLen]),
		btime.ParseTimeMS(data.TimeRangeRaw[cutLen+1:]),
	}
	Name = data.Name
	core.RunEnv = data.Env
	core.RunMode = data.RunMode
	Leverage = data.Leverage
	LimitVolSecs = data.LimitVolSecs
	core.ExgName = data.Exchange.Name
	core.Market = data.MarketType
	if core.Market == banexg.MarketSpot || core.Market == banexg.MarketMargin {
		data.ContractType = ""
	} else if data.ContractType == "" {
		data.ContractType = banexg.MarketSwap
	}
	core.ContractType = data.ContractType
	MaxMarketRate = data.MaxMarketRate
	OdBookTtl = data.OdBookTtl
	OrderType = data.OrderType
	PreFire = data.PreFire
	MarginAddRate = data.MarginAddRate
	ChargeOnBomb = data.ChargeOnBomb
	AutoEditLimit = data.AutoEditLimit
	TakeOverStgy = data.TakeOverStgy
	StakeAmount = data.StakeAmount
	MinOpenRate = data.MinOpenRate
	MaxOpenOrders = data.MaxOpenOrders
	WalletAmounts = data.WalletAmounts
	DrawBalanceOver = data.DrawBalanceOver
	StakeCurrency = data.StakeCurrency
	FatalStop = data.FatalStop
	FatalStopHours = data.FatalStopHours
	TimeRange = data.TimeRange
	WsStamp = data.WsStamp
	RunTimeframes = data.RunTimeframes
	KlineSource = data.KlineSource
	WatchJobs = data.WatchJobs
	RunPolicy = data.RunPolicy
	Pairs = data.Pairs
	PairMgr = data.PairMgr
	PairFilters = data.PairFilters
	Exchange = data.Exchange
	ExgDataMap = data.ExgDataMap
	Database = data.Database
	SpiderAddr = data.SpiderAddr
	APIServer = data.APIServer
	RPCChannels = data.RPCChannels
	Webhook = data.Webhook
	return nil
}

func GetExgConfig() *ExgItemConfig {
	if cfg, ok := Exchange.Items[Exchange.Name]; ok {
		return cfg
	}
	return &ExgItemConfig{}
}
