package config

import (
	"fmt"
	"github.com/anyongjin/banbot/cmd"
	"github.com/anyongjin/banbot/log"
	"github.com/anyongjin/banbot/utils"
	"github.com/mitchellh/mapstructure"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
)

func IsLiveMode(mode string) bool {
	return mode == RunModeProd || mode == RunModeDryRun
}

func LiveMode() bool {
	return IsLiveMode(run_mode)
}

func GetDataDir() string {
	dataDir := os.Getenv("ban_data_dir")
	if dataDir != "" {
		return dataDir
	}
	if Cfg.DataDir != "" {
		return Cfg.DataDir
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
		return RunEnvTest
	} else if runEnv != RunEnvTest && runEnv != RunEnvProd {
		log.Error(fmt.Sprintf("invalid run env: %v", runEnv))
		return RunEnvTest
	}
	return runEnv
}

func LoadConfig(args *cmd.CmdArgs) (*Config, error) {
	if Cfg.Loaded {
		return &Cfg, nil
	}
	runEnv := GetRunEnv()
	if runEnv != RunEnvProd {
		log.Info("Running in test, Please set `ban_run_env=prod` in production running")
	}
	var paths []string
	if !args.NoDefault {
		dataDir := GetDataDir()
		if dataDir == "" {
			return nil, ErrDataDirInvalid
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
			return nil, fmt.Errorf("Read %s Fail: %v", path, err)
		}
		var unpak map[interface{}]interface{}
		err = yaml.Unmarshal(data, &unpak)
		if err != nil {
			return nil, fmt.Errorf("Unmarshal %s Fail: %v", path, err)
		}
		utils.DeepCopy(unpak, merged)
	}
	err := mapstructure.Decode(merged, &Cfg)
	if err != nil {
		return nil, fmt.Errorf("decode Config Fail: %v", err)
	}
	Cfg.Loaded = true
	Cfg.Debug = args.Debug
	run_mode = Cfg.RunMode
	return &Cfg, nil
}
