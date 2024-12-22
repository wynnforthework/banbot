package dev

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	utils2 "github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"

	"github.com/banbox/banbot/core"

	"github.com/banbox/banexg/log"
	"go.uber.org/zap"

	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/orm/ormu"
	"github.com/banbox/banexg/utils"
)

type CmdArgs struct {
	Port     int
	Host     string
	Configs  config.ArrString
	DataDir  string
	LogLevel string
	LogFile  string
	TimeZone string
	DBFile   string
}

var (
	btInfoKeyList = []string{"maxOpenOrders", "showDrawDownPct", "barNum", "maxDrawDownVal", "showDrawDownVal", "totalInvest",
		"totProfit", "totCost", "totFee", "totProfitPct", "sortinoRatio"}
	btInfoKeys      = make(map[string]bool)
	maxBtTasks      = 3 // 最大并发回测任务数
	runBtTasks      = make(map[int64]*exec.Cmd)
	runBtTasksMutex sync.Mutex
)

func init() {
	for _, k := range btInfoKeyList {
		btInfoKeys[k] = true
	}
	startBtTaskScheduler()
}

// 启动后台任务处理
func startBtTaskScheduler() {
	btRoot := fmt.Sprintf("%s/backtest", config.GetDataDir())
	go func() {
		for {
			time.Sleep(100 * time.Millisecond)
			runBtTasksMutex.Lock()
			runningCount := len(runBtTasks)
			runBtTasksMutex.Unlock()

			if runningCount >= maxBtTasks {
				continue
			}

			// 获取待执行的任务
			qu, err := ormu.Conn()
			if err != nil {
				log.Error("connect to db failed", zap.Error(err))
				continue
			}

			tasks, err := qu.FindTasks(context.Background(), ormu.FindTasksParams{
				Mode:   "backtest",
				Status: ormu.BtStatusInit,
				Limit:  1,
			})
			if err != nil {
				log.Error("find tasks failed", zap.Error(err))
				continue
			}

			if len(tasks) == 0 {
				continue
			}

			task := tasks[0]
			runBtTasksMutex.Lock()
			_, exist := runBtTasks[task.ID]
			if !exist {
				runBtTasks[task.ID] = nil
			}
			runBtTasksMutex.Unlock()
			if exist {
				continue
			}
			// 启动新的回测任务
			go func(t *ormu.Task) {
				defer func() {
					runBtTasksMutex.Lock()
					delete(runBtTasks, t.ID)
					runBtTasksMutex.Unlock()
				}()
				// 获取当前执行文件路径
				exePath, err := os.Executable()
				if err != nil {
					log.Error("get executable path failed", zap.Error(err))
					return
				}

				// 构建命令
				cmdArgsStr := "backtest " + t.Args
				cmd := exec.Command(exePath, strings.Split(cmdArgsStr, " ")...)

				// 添加到运行列表
				runBtTasksMutex.Lock()
				runBtTasks[t.ID] = cmd
				runBtTasksMutex.Unlock()

				// 更新任务状态为运行中
				err = qu.UpdateTask(context.Background(), ormu.UpdateTaskParams{
					Status: ormu.BtStatusRunning,
					ID:     t.ID,
				})
				if err != nil {
					log.Error("update task status failed", zap.Error(err))
					return
				}

				// 执行命令
				log.Info("start backtest", zap.Int64("id", task.ID), zap.String("args", cmdArgsStr))
				output, err := cmd.CombinedOutput()
				if err != nil {
					log.Error("run backtest failed", zap.String("path", t.Path), zap.String("output", string(output)), zap.Error(err))
				} else {
					log.Info("done backtest", zap.Int64("id", task.ID), zap.String("args", cmdArgsStr))
				}

				// 更新任务状态
				task, err = collectBtTask(filepath.Join(btRoot, t.Path))
				if err != nil {
					log.Error("collect backtest task failed", zap.Error(err))
					return
				}
				if task == nil {
					task = &ormu.Task{}
				}
				err = qu.UpdateTask(context.Background(), ormu.UpdateTaskParams{
					Status:      ormu.BtStatusDone,
					OrderNum:    task.OrderNum,
					ProfitRate:  task.ProfitRate,
					WinRate:     task.WinRate,
					MaxDrawdown: task.MaxDrawdown,
					Sharpe:      task.Sharpe,
					Info:        task.Info,
					ID:          t.ID,
				})
				if err != nil {
					log.Error("update task status failed", zap.Error(err))
				}
			}(task)
		}
	}()
}

func collectBtResults() error {
	qu, err2 := ormu.Conn()
	if err2 != nil {
		return err2
	}
	tasks, err2 := qu.FindTasks(context.Background(), ormu.FindTasksParams{
		Mode:  "backtest",
		Limit: 1000,
	})
	if err2 != nil {
		return err2
	}
	taskMap := make(map[string]*ormu.Task)
	for _, t := range tasks {
		taskMap[t.Path] = t
	}

	addNum, delNum := 0, 0
	btRoot := fmt.Sprintf("%s/backtest", config.GetDataDir())
	err := filepath.Walk(btRoot, func(fullPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() || fullPath == btRoot {
			return nil
		}

		relPath, err := filepath.Rel(btRoot, fullPath)
		if err != nil {
			return err
		}
		if _, ok := taskMap[relPath]; ok {
			delete(taskMap, relPath)
			return nil
		}

		task, err := collectBtTask(fullPath)
		if err != nil || task == nil {
			return err
		}

		_, err = qu.AddTask(context.Background(), ormu.AddTaskParams{
			Mode:        task.Mode,
			Path:        task.Path,
			Strats:      task.Strats,
			Periods:     task.Periods,
			Pairs:       task.Pairs,
			CreateAt:    task.CreateAt,
			StartAt:     task.StartAt,
			StopAt:      task.StopAt,
			Status:      task.Status,
			OrderNum:    task.OrderNum,
			ProfitRate:  task.ProfitRate,
			WinRate:     task.WinRate,
			MaxDrawdown: task.MaxDrawdown,
			Sharpe:      task.Sharpe,
			Info:        task.Info,
		})
		addNum += 1
		return err
	})
	if err != nil {
		return err
	}
	if len(taskMap) > 0 {
		delIds := make([]int64, 0, len(taskMap))
		for _, t := range taskMap {
			delIds = append(delIds, t.ID)
		}
		delNum = len(delIds)
		err = qu.DelTasks(context.Background(), delIds)
	}

	log.Info("collect backtest tasks", zap.Int("add", addNum), zap.Int("del", delNum))

	return err
}

func collectBtTask(btDir string) (*ormu.Task, error) {
	tradesDB := filepath.Join(btDir, "trades.db")
	tradesInfo, err := os.Stat(tradesDB)
	if os.IsNotExist(err) {
		return nil, nil
	}

	// 读取并解析 detail.json
	detailPath := filepath.Join(btDir, "detail.json")
	detailBytes, err := os.ReadFile(detailPath)
	if err != nil {
		return nil, nil // 如果detail.json不存在则跳过
	}

	var data = make(map[string]interface{})
	if err = utils.Unmarshal(detailBytes, &data, utils.JsonNumAuto); err != nil {
		return nil, nil
	}

	cfg, err2 := config.GetConfig(&config.CmdArgs{Configs: []string{
		filepath.Join(btDir, "config.yml"),
	}}, false)
	if err2 != nil {
		return nil, err2
	}

	d := make(map[string]interface{})
	for k := range btInfoKeys {
		d[k] = data[k]
	}
	d["leverage"] = cfg.Leverage
	walletTot := float64(0)
	for code, amt := range cfg.WalletAmounts {
		walletTot += core.GetPriceSafe(code) * amt
	}
	d["walletAmount"] = walletTot
	d["stakeAmount"] = cfg.StakeAmount
	infoText, err := utils.MarshalString(d)
	if err != nil {
		return nil, err
	}
	dayMSecs := int64(utils.TFToSecs("1d") * 1000)
	return &ormu.Task{
		Mode:        "backtest",
		Path:        filepath.Base(btDir),
		Strats:      strings.Join(cfg.Strats(), ","),
		Periods:     strings.Join(cfg.TimeFrames(), ","),
		Pairs:       cfg.ShowPairs(),
		CreateAt:    tradesInfo.ModTime().UnixMilli(),
		StartAt:     utils.AlignTfMSecs(cfg.TimeRange.StartMS, dayMSecs),
		StopAt:      utils.AlignTfMSecs(cfg.TimeRange.EndMS, dayMSecs),
		Status:      ormu.BtStatusDone,
		OrderNum:    utils.GetMapVal(data, "orderNum", int64(0)),
		ProfitRate:  utils.GetMapVal(data, "totProfitPct", float64(0)),
		WinRate:     utils.GetMapVal(data, "winRatePct", float64(0)),
		MaxDrawdown: utils.GetMapVal(data, "maxDrawDownPct", float64(0)),
		Sharpe:      utils.GetMapVal(data, "sharpeRatio", float64(0)),
		Info:        infoText,
	}, nil
}

func MergeConfig(inText string) (string, error) {
	dataDir := config.GetDataDir()
	if dataDir == "" {
		return "", errs.NewMsg(errs.CodeParamRequired, "data_dir is empty")
	}
	tryNames := []string{"config.yml", "config.local.yml"}
	var paths []string
	for _, name := range tryNames {
		path := filepath.Join(dataDir, name)
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}
	if inText != "" {
		tmp, err := os.CreateTemp(os.TempDir(), "tmp_cfg")
		if err != nil {
			return "", err
		}
		defer os.Remove(tmp.Name())
		tmp.WriteString(inText)
		paths = append(paths, tmp.Name())
	}
	var content string
	var err error
	if len(paths) > 1 {
		content, err = utils2.MergeYamlStr(paths, make(map[string]bool))
		if err != nil {
			return "", err
		}
	} else if len(paths) == 1 {
		var data []byte
		data, err = os.ReadFile(paths[0])
		if err != nil {
			return "", err
		}
		content = string(data)
	}
	return content, nil
}
