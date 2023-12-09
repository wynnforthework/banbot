package config

import (
	"fmt"
	"github.com/anyongjin/banbot/btime"
	"github.com/anyongjin/banbot/cmd"
	"github.com/anyongjin/banbot/core"
	"github.com/anyongjin/banbot/log"
	"github.com/anyongjin/banbot/utils"
	"github.com/mitchellh/mapstructure"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
)

func IsLiveMode(mode string) bool {
	return mode == core.RunModeProd || mode == core.RunModeDryRun
}

func LiveMode() bool {
	return IsLiveMode(RunMode)
}

/*
ProdMode
提交到交易所模式
*/
func ProdMode() bool {
	return RunMode == core.RunModeProd
}

func GetDataDir() string {
	dataDir := os.Getenv("ban_data_dir")
	if dataDir != "" {
		return dataDir
	}
	if config.DataDir != "" {
		return config.DataDir
	}
	return ""
}

func GetStagyDir() string {
	result := os.Getenv("ban_stg_dir")
	if result == "" {
		panic(fmt.Errorf("`ban_stg_dir` env is required"))
	}
	return result
}

func GetRunEnv() string {
	runEnv := os.Getenv("ban_run_env")
	if runEnv == "" {
		return core.RunEnvTest
	} else if runEnv != core.RunEnvTest && runEnv != core.RunEnvProd {
		log.Error(fmt.Sprintf("invalid run env: %v", runEnv))
		return core.RunEnvTest
	}
	return runEnv
}

func LoadConfig(args *cmd.CmdArgs) error {
	if Loaded {
		return nil
	}
	runEnv := GetRunEnv()
	if runEnv != core.RunEnvProd {
		log.Info("Running in test, Please set `ban_run_env=prod` in production running")
	}
	var paths []string
	if !args.NoDefault {
		dataDir := GetDataDir()
		if dataDir == "" {
			return ErrDataDirInvalid
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
	var merged = make(map[interface{}]interface{})
	for _, path := range paths {
		log.Info("Using ", zap.String("config", path))
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("Read %s Fail: %v", path, err)
		}
		var unpak map[interface{}]interface{}
		err = yaml.Unmarshal(data, &unpak)
		if err != nil {
			return fmt.Errorf("Unmarshal %s Fail: %v", path, err)
		}
		utils.DeepCopy(unpak, merged)
	}
	err := mapstructure.Decode(merged, &config)
	if err != nil {
		return fmt.Errorf("decode Config Fail: %v", err)
	}
	apply(args)
	return nil
}

func apply(args *cmd.CmdArgs) {
	Loaded = true
	Debug = args.Debug
	NoDB = args.NoDb
	if args.TimeRange != "" {
		config.TimeRangeRaw = args.TimeRange
	}
	cutLen := len(config.TimeRangeRaw) / 2
	config.TimeRange = &TimeTuple{
		btime.ParseTimeMS(config.TimeRangeRaw[:cutLen]),
		btime.ParseTimeMS(config.TimeRangeRaw[cutLen+1:]),
	}
	Name = config.Name
	Env = config.Env
	RunMode = config.RunMode
	Leverage = config.Leverage
	LimitVolSecs = config.LimitVolSecs
	MarketType = config.MarketType
	MaxMarketRate = config.MaxMarketRate
	OdBookTtl = config.OdBookTtl
	OrderType = config.OrderType
	PreFire = config.PreFire
	MarginAddRate = config.MarginAddRate
	ChargeOnBomb = config.ChargeOnBomb
	AutoEditLimit = config.AutoEditLimit
	TakeOverStgy = config.TakeOverStgy
	StakeAmount = config.StakeAmount
	MinOpenRate = config.MinOpenRate
	MaxOpenOrders = config.MaxOpenOrders
	WalletAmounts = config.WalletAmounts
	DrawBalanceOver = config.DrawBalanceOver
	StakeCurrency = config.StakeCurrency
	FatalStop = config.FatalStop
	FatalStopHours = config.FatalStopHours
	TimeRange = config.TimeRange
	WsStamp = config.WsStamp
	RunTimeframes = config.RunTimeframes
	KlineSource = config.KlineSource
	WatchJobs = config.WatchJobs
	RunPolicy = config.RunPolicy
	Pairs = config.Pairs
	PairMgr = config.PairMgr
	PairFilters = config.PairFilters
	Exchange = config.Exchange
	DataDir = config.DataDir
	ExgDataMap = config.ExgDataMap
	Database = config.Database
	SpiderAddr = config.SpiderAddr
	APIServer = config.APIServer
	RPCChannels = config.RPCChannels
	Webhook = config.Webhook
}
