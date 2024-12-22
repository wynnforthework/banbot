package dev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	utils2 "github.com/banbox/banexg/utils"

	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/opt"
	"github.com/banbox/banbot/orm/ormo"
	"github.com/banbox/banbot/orm/ormu"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banbot/web/base"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"

	"github.com/banbox/banbot/core"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

// FileNode 表示文件树的节点
type FileNode struct {
	Path  string `json:"path"`            // 相对路径
	Size  int64  `json:"size,omitempty"`  // 文件大小（文件夹时忽略）
	Stamp int64  `json:"stamp,omitempty"` // 最后修改时间戳（文件夹时忽略）
}

// 添加一个互斥锁来控制编译状态
var buildMutex sync.Mutex

func regApiDev(api fiber.Router) {
	api.Get("/ws", websocket.New(onWsDev))
	api.Get("/orders", getOrders)
	api.Get("/strat_tree", getStratTree)
	api.Get("/bt_tasks", getBtTasks)
	api.Get("/bt_options", getBtOptions)
	api.Post("/file_op", handleFileOp)
	api.Post("/new_strat", handleNewStrat)
	api.Get("/text", getText)
	api.Post("/save_text", saveText)
	api.Post("/build", handleBuild)
	api.Get("/logs", getLogs)
	api.Get("/default_cfg", getDefaultCfg)
	api.Post("/run_backtest", handleRunBacktest)
	api.Get("/bt_detail", getBtDetail)
	api.Get("/bt_orders", getBtOrders)
	api.Get("/bt_config", getBtConfig)
	api.Get("/bt_logs", getBtLogs)
	api.Get("/bt_html", getBtHtml)
}

func onWsDev(c *websocket.Conn) {
	NewWsClient(c).HandleForever()
}

func getOrders(c *fiber.Ctx) error {
	type OrderArgs struct {
		TaskID int64 `query:"task_id" validate:"required"`
	}

	var data = new(OrderArgs)
	if err := base.VerifyArg(c, data, base.ArgQuery); err != nil {
		return err
	}

	qu, err2 := ormu.Conn()
	if err2 != nil {
		return err2
	}
	task, err := qu.GetTask(context.Background(), data.TaskID)
	if err != nil {
		return err
	}

	sess, err2 := ormo.Conn(task.Path, false)
	if err2 != nil {
		return err2
	}
	orders, err2 := sess.GetOrders(ormo.GetOrdersArgs{
		TaskID: data.TaskID,
	})
	if err2 != nil {
		return err2
	}

	return c.JSON(fiber.Map{
		"data": orders,
	})
}

func getStratTree(c *fiber.Ctx) error {
	baseDir, err := getRootDir()
	if err != nil {
		return err
	}

	var files []FileNode
	// 遍历目录
	err = filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// 计算相对路径
		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}
		relPath = strings.ReplaceAll(relPath, "\\", "/")

		if info.IsDir() {
			// 对于目录，添加末尾斜杠
			files = append(files, FileNode{
				Path: relPath + "/",
			})
		} else {
			files = append(files, FileNode{
				Path:  relPath,
				Size:  info.Size(),
				Stamp: info.ModTime().UnixMilli(),
			})
		}

		return nil
	})

	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"data": files,
	})
}

// handleFileOp 处理文件操作请求
func handleFileOp(c *fiber.Ctx) error {
	type FileOp struct {
		Op     string `json:"op"`
		Path   string `json:"path"`
		Target string `json:"target,omitempty"`
	}
	var op = new(FileOp)
	if err := base.VerifyArg(c, op, base.ArgBody); err != nil {
		return err
	}

	baseDir, err := getRootDir()
	if err != nil {
		return err
	}
	srcPath := filepath.Join(baseDir, op.Path)

	switch op.Op {
	case "newFile":
		newPath := filepath.Join(srcPath, op.Target)
		file, err := os.Create(newPath)
		if err != nil {
			return err
		}
		file.Close()

	case "newFolder":
		newPath := filepath.Join(srcPath, op.Target)
		if err = os.MkdirAll(newPath, 0755); err != nil {
			return err
		}

	case "rename":
		newPath := filepath.Join(filepath.Dir(srcPath), op.Target)
		if err = os.Rename(srcPath, newPath); err != nil {
			return err
		}

	case "cut":
		targetPath := filepath.Join(baseDir, op.Target, filepath.Base(srcPath))
		if err = utils.MovePath(srcPath, targetPath); err != nil {
			return err
		}

	case "copy":
		targetPath := filepath.Join(baseDir, op.Target, filepath.Base(srcPath))
		if err = utils.CopyDir(srcPath, targetPath); err != nil {
			return err
		}

	case "delete":
		if err = os.RemoveAll(srcPath); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unsupport operation: %s", op.Op)
	}

	return c.JSON(fiber.Map{
		"code": 200,
	})
}

func handleNewStrat(c *fiber.Ctx) error {
	type NewStratArgs struct {
		Folder string `json:"folder" validate:"required"`
		Name   string `json:"name" validate:"required"`
	}

	var args = new(NewStratArgs)
	if err := base.VerifyArg(c, args, base.ArgBody); err != nil {
		return err
	}
	err := makeNewStrat(args.Folder, args.Name)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"code": 200,
	})
}

func getText(c *fiber.Ctx) error {
	type TextArgs struct {
		Path string `query:"path" validate:"required"`
	}

	var args = new(TextArgs)
	if err := base.VerifyArg(c, args, base.ArgQuery); err != nil {
		return err
	}

	baseDir, err := getRootDir()
	if err != nil {
		return err
	}

	filePath := filepath.Join(baseDir, args.Path)

	// 检查文件是否存在
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(400).JSON(fiber.Map{
				"msg": "File not found",
			})
		}
		return err
	}

	// 检查是否是目录
	if info.IsDir() {
		return c.Status(400).JSON(fiber.Map{
			"msg": "File is a directory",
		})
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	if !utils.IsTextContent(content) {
		return c.Status(400).JSON(fiber.Map{
			"msg": "File is not a text file",
		})
	}

	return c.JSON(fiber.Map{
		"data": string(content),
	})
}

func saveText(c *fiber.Ctx) error {
	type SaveTextArgs struct {
		Path    string `json:"path" validate:"required"`
		Content string `json:"content" validate:"required"`
	}

	var args = new(SaveTextArgs)
	if err := base.VerifyArg(c, args, base.ArgBody); err != nil {
		return err
	}

	// 检查内容是否为空
	if len(strings.TrimSpace(args.Content)) == 0 {
		return c.Status(400).JSON(fiber.Map{
			"msg": "Content cannot be empty",
		})
	}

	baseDir, err := getRootDir()
	if err != nil {
		return err
	}

	filePath := filepath.Join(baseDir, args.Path)

	// 检查文件是否存在
	_, err = os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(400).JSON(fiber.Map{
				"msg": "File not found",
			})
		}
		return err
	}

	// 写入文件内容
	err = os.WriteFile(filePath, []byte(args.Content), 0644)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"code": 200,
	})
}

// handleBuild 处理编译请求
func handleBuild(c *fiber.Ctx) error {
	// 检查是否正在编译
	buildMutex.Lock()
	if status.Building {
		buildMutex.Unlock()
		return c.Status(400).JSON(fiber.Map{
			"msg": "Another build is in progress",
		})
	}
	status.Building = true
	BroadcastStatus()
	buildMutex.Unlock()

	// 在函数结束时确保重置编译状态
	defer func() {
		buildMutex.Lock()
		status.Building = false
		BroadcastStatus()
		buildMutex.Unlock()
	}()

	// 获取根目录
	rootDir, err := getRootDir()
	if err != nil {
		return err
	}

	// 获取可执行文件名称
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exeName := path.Base(exePath)

	// 准备编译命令
	cmd := exec.Command("go", "build", "-o", exeName)
	cmd.Dir = rootDir

	// 设置环境变量
	env := os.Environ()
	env = append(env, fmt.Sprintf("GOARCH=amd64"))
	env = append(env, fmt.Sprintf("GOOS=%s", runtime.GOOS))
	cmd.Env = env

	// 捕获命令输出
	output, err := cmd.CombinedOutput()

	if err != nil {
		log.Warn("Build failed", zap.Error(err), zap.String("output", string(output)))
		return c.Status(500).JSON(fiber.Map{
			"msg": fmt.Sprintf("Build failed: %v", err),
		})
	}

	if len(output) > 0 {
		log.Info("Build success", zap.String("output", string(output)))
	} else {
		log.Info("Build completed successfully")
	}

	return c.JSON(fiber.Map{
		"code": 200,
	})
}

func getLogs(c *fiber.Ctx) error {
	type LogArgs struct {
		End   int64 `query:"end"`
		Limit int   `query:"limit"`
	}

	var args = new(LogArgs)
	if err := base.VerifyArg(c, args, base.ArgQuery); err != nil {
		return err
	}
	if args.Limit <= 0 {
		args.Limit = 1000
	}

	file, err := os.Open(core.LogFile)
	if err != nil {
		return err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}
	fileSize := fileInfo.Size()

	// 读取日志内容
	buffer := make([]byte, 4096)
	var lines []string
	var pos = fileSize
	if args.End > 0 && args.End < fileSize {
		pos = args.End
	}
	for len(lines) < args.Limit && pos > 0 {
		// 计算本次读的大小和位置
		readSize := int64(len(buffer))
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize

		// 读取数据
		_, err = file.Seek(pos, 0)
		if err != nil {
			return err
		}
		n, err := file.Read(buffer[:readSize])
		if err != nil && err != io.EOF {
			return err
		}

		// 将内容按行分隔
		content := string(buffer[:n])
		newLines := strings.Split(content, "\n")

		// 处理跨越读取边界的行
		if len(lines) > 0 && len(newLines) > 0 {
			lines[0] = newLines[len(newLines)-1] + lines[0]
			newLines = newLines[:len(newLines)-1]
		}

		// 将新行添加到结果中
		merge := make([]string, 0, len(lines)+len(newLines))
		merge = append(merge, newLines...)
		merge = append(merge, lines...)
		lines = merge
	}

	return c.JSON(fiber.Map{
		"data":  strings.Join(lines, "\n"),
		"start": pos,
	})
}

// getBtTasks 获取回测任务列表
func getBtTasks(c *fiber.Ctx) error {
	type TaskArgs struct {
		Mode     string `query:"mode"`
		Path     string `query:"path"`
		Strat    string `query:"strat"`
		Period   string `query:"period"`
		RangeStr string `query:"range"`
		MinStart int64  `query:"minStart"`
		MaxStart int64  `query:"maxStart"`
		MaxID    int64  `query:"maxId"`
		Limit    int64  `query:"limit"`
	}

	var args = new(TaskArgs)
	if err := base.VerifyArg(c, args, base.ArgQuery); err != nil {
		return err
	}

	if args.Limit <= 0 {
		args.Limit = 20
	}

	qu, err := ormu.Conn()
	if err != nil {
		return err
	}

	var startMS, endMS int64
	if args.RangeStr != "" {
		parts := strings.Split(args.RangeStr, "-")
		if len(parts) == 2 {
			startMS = btime.ParseTimeMS(parts[0])
			endMS = btime.ParseTimeMS(parts[1])
		}
	}

	tasks, err := qu.FindTasks(context.Background(), ormu.FindTasksParams{
		Mode:     args.Mode,
		Path:     args.Path,
		Strat:    args.Strat,
		Period:   args.Period,
		StartAt:  startMS,
		StopAt:   endMS,
		MinStart: args.MinStart,
		MaxStart: args.MaxStart,
		MaxID:    args.MaxID,
		Limit:    args.Limit,
	})
	if err != nil {
		return err
	}

	// 处理返回数据
	result := make([]map[string]interface{}, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, task.ToMap())
	}

	return c.JSON(fiber.Map{
		"data": result,
	})
}

// getBtOptions 获取回测选项列表
func getBtOptions(c *fiber.Ctx) error {
	qu, err2 := ormu.Conn()
	if err2 != nil {
		return err2
	}

	options, err := qu.GetTaskOptions(context.Background())
	if err != nil {
		return err
	}

	// 处理策略列表
	stratMap := make(map[string]bool)
	for _, o := range options {
		strats := strings.Split(o.Strats, ",")
		for _, s := range strats {
			if s = strings.TrimSpace(s); s != "" {
				stratMap[s] = true
			}
		}
	}
	strats := make([]string, 0, len(stratMap))
	for s := range stratMap {
		strats = append(strats, s)
	}
	sort.Strings(strats)

	// 处理周期列表
	periodMap := make(map[string]int)
	for _, o := range options {
		periods := strings.Split(o.Periods, ",")
		for _, p := range periods {
			if p = strings.TrimSpace(p); p != "" {
				periodMap[p] = utils2.TFToSecs(p)
			}
		}
	}
	periods := make([]string, 0, len(periodMap))
	for p := range periodMap {
		periods = append(periods, p)
	}
	sort.SliceStable(periods, func(i, j int) bool {
		return periodMap[periods[i]] < periodMap[periods[j]]
	})

	// 处理日期范围
	dateMap := make(map[string]bool)
	for _, o := range options {
		if o.StartAt > 0 && o.StopAt > 0 {
			startStr := btime.ToDateStr(o.StartAt, "20060102")
			stopStr := btime.ToDateStr(o.StopAt, "20060102")
			dateRange := fmt.Sprintf("%s-%s", startStr, stopStr)
			dateMap[dateRange] = true
		}
	}
	dates := make([]string, 0, len(dateMap))
	for d := range dateMap {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	return c.JSON(fiber.Map{
		"strats":  strats,
		"periods": periods,
		"ranges":  dates,
	})
}

func getDefaultCfg(c *fiber.Ctx) error {
	content, err := MergeConfig("")
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"data": content,
	})
}

// handleRunBacktest 处理回测请求
func handleRunBacktest(c *fiber.Ctx) error {
	type RunBtArgs struct {
		Separate bool   `json:"separate"`
		Config   string `json:"config" validate:"required"`
	}

	var args = new(RunBtArgs)
	if err := base.VerifyArg(c, args, base.ArgBody); err != nil {
		return err
	}

	// 创建临时文件存储配置
	tmpFile, err := os.CreateTemp(os.TempDir(), "tmp_cfg_*.yml")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// 写入配置内容
	if _, err = tmpFile.WriteString(args.Config); err != nil {
		return err
	}
	tmpFile.Close()

	// 加载并验证配置
	cfg, err2 := config.GetConfig(&config.CmdArgs{
		Configs: []string{tmpPath},
	}, false)
	if err2 != nil {
		return err2
	}

	// 检查必要的配置项
	if len(cfg.Pairs) == 0 {
		return errs.NewMsg(errs.CodeParamRequired, "pairs is required")
	}
	if len(cfg.RunPolicy) == 0 {
		return errs.NewMsg(errs.CodeParamRequired, "run_policy is required")
	}
	if cfg.TimeRange.StartMS == 0 || cfg.TimeRange.EndMS == 0 {
		return errs.NewMsg(errs.CodeParamRequired, "time_range is required")
	}
	if len(cfg.WalletAmounts) == 0 {
		return errs.NewMsg(errs.CodeParamRequired, "wallet_amounts is required")
	}
	if cfg.StakeAmount == 0 {
		return errs.NewMsg(errs.CodeParamRequired, "stake_amount is required")
	}
	if len(cfg.StakeCurrency) == 0 {
		return errs.NewMsg(errs.CodeParamRequired, "stake_currency is required")
	}
	if cfg.Exchange.Name == "" {
		return errs.NewMsg(errs.CodeParamRequired, "exchange.name is required")
	}
	if cfg.Database.Url == "" {
		return errs.NewMsg(errs.CodeParamRequired, "database.url is required")
	}

	// 获取配置内容并计算哈希
	cfgData, err2 := cfg.DumpYaml()
	if err2 != nil {
		return err2
	}
	hashVal := utils.MD5(cfgData)[:10]
	btPath := fmt.Sprintf("$/backtest/%s", hashVal)
	absPath := config.ParsePath(btPath)

	// 创建目标目录
	if err = os.MkdirAll(absPath, 0755); err != nil {
		return err
	}

	// 移动配置文件
	cfgPath := filepath.Join(absPath, "config.yml")
	if err = os.WriteFile(cfgPath, cfgData, 0644); err != nil {
		return err
	}

	// 构建回测参数
	btArgs := fmt.Sprintf("-out %s -config %s", btPath, btPath+"/config.yml")
	if args.Separate {
		btArgs = "-separate " + btArgs
	}

	// 添加回测任务
	qu, err2 := ormu.Conn()
	if err2 != nil {
		return err2
	}

	task, err := qu.AddTask(context.Background(), ormu.AddTaskParams{
		Mode:     "backtest",
		Path:     hashVal,
		Args:     btArgs,
		Config:   string(cfgData),
		Strats:   strings.Join(cfg.Strats(), ","),
		Periods:  strings.Join(cfg.TimeFrames(), ","),
		Pairs:    cfg.ShowPairs(),
		CreateAt: btime.UTCStamp(),
		StartAt:  cfg.TimeRange.StartMS,
		StopAt:   cfg.TimeRange.EndMS,
		Status:   ormu.BtStatusInit,
	})
	if err != nil {
		return err
	}
	log.Info("add backtest", zap.Int64("id", task.ID), zap.String("hash", hashVal))

	return c.JSON(fiber.Map{
		"code": 200,
		"data": hashVal,
	})
}

// getBtPath 获取回测输出目录
func getBtPath(taskID int64) (string, error) {
	qu, err2 := ormu.Conn()
	if err2 != nil {
		return "", err2
	}
	task, err := qu.GetTask(context.Background(), taskID)
	if err != nil {
		return "", fmt.Errorf("query task failed: %v", err)
	}
	return filepath.Join(config.GetDataDir(), "backtest", task.Path), nil
}

// parseBtResult 解析回测结果
func parseBtResult(path string) (*opt.BTResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file failed: %v", err)
	}
	var res = new(opt.BTResult)
	err = json.Unmarshal(data, res)
	if err != nil {
		return nil, fmt.Errorf("unmarshal json failed: %v", err)
	}
	return res, nil
}

// getBtDetail 获取回测详情
func getBtDetail(c *fiber.Ctx) error {
	type DetailArgs struct {
		TaskID int64 `query:"task_id" validate:"required"`
	}
	var args = new(DetailArgs)
	if err := base.VerifyArg(c, args, base.ArgQuery); err != nil {
		return err
	}

	qu, err2 := ormu.Conn()
	if err2 != nil {
		return err2
	}
	task, err := qu.GetTask(context.Background(), args.TaskID)
	if err != nil {
		return fmt.Errorf("query task failed: %v", err)
	}
	btPath := filepath.Join(config.GetDataDir(), "backtest", task.Path)

	// 读取detail.json
	detailPath := filepath.Join(btPath, "detail.json")
	detail, err := parseBtResult(detailPath)
	if err != nil {
		return fmt.Errorf("parse backtest result failed: %v", err)
	}

	return c.JSON(fiber.Map{
		"path":   btPath,
		"detail": detail,
		"task":   task.ToMap(),
	})
}

// getBtOrders 获取回测订单
func getBtOrders(c *fiber.Ctx) error {
	type OrderArgs struct {
		TaskID   int64  `query:"task_id" validate:"required"`
		Page     int    `query:"page"`
		PageSize int    `query:"page_size"`
		Symbol   string `query:"symbol"`
		Strategy string `query:"strategy"`
		EnterTag string `query:"enter_tag"`
		ExitTag  string `query:"exit_tag"`
		StartMS  int64  `query:"start_ms"`
		EndMS    int64  `query:"end_ms"`
	}
	var args = new(OrderArgs)
	if err := base.VerifyArg(c, args, base.ArgQuery); err != nil {
		return err
	}

	if args.Page <= 0 {
		args.Page = 1
	}
	if args.PageSize <= 0 {
		args.PageSize = 20
	}

	qu, err2 := ormu.Conn()
	if err2 != nil {
		return err2
	}
	task, err := qu.GetTask(context.Background(), args.TaskID)
	if err != nil {
		return fmt.Errorf("query task failed: %v", err)
	}

	dbPath := filepath.Join(config.GetDataDir(), "backtest", task.Path, "trades.db")
	sess, err2 := ormo.Conn(dbPath, false)
	if err2 != nil {
		return err2
	}

	var symbols []string
	if args.Symbol != "" {
		symbols = append(symbols, args.Symbol)
	}
	orders, err2 := sess.GetOrders(ormo.GetOrdersArgs{
		Pairs:       symbols,
		Strategy:    args.Strategy,
		EnterTag:    args.EnterTag,
		ExitTag:     args.ExitTag,
		CloseAfter:  args.StartMS,
		CloseBefore: args.EndMS,
	})
	if err2 != nil {
		return err2
	}

	total := len(orders)
	start := (args.Page - 1) * args.PageSize
	end := start + args.PageSize
	if end > total {
		end = total
	}
	if start >= total {
		orders = []*ormo.InOutOrder{}
	} else {
		orders = orders[start:end]
	}

	return c.JSON(fiber.Map{
		"total":  total,
		"orders": orders,
	})
}

// getBtConfig 获取回测配置
func getBtConfig(c *fiber.Ctx) error {
	type ConfigArgs struct {
		TaskID int64 `query:"task_id" validate:"required"`
	}
	var args = new(ConfigArgs)
	if err := base.VerifyArg(c, args, base.ArgQuery); err != nil {
		return err
	}

	btPath, err := getBtPath(args.TaskID)
	if err != nil {
		return fmt.Errorf("get backtest path failed: %v", err)
	}

	// 读取config.yml
	configPath := filepath.Join(btPath, "config.yml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config file failed: %v", err)
	}

	return c.JSON(fiber.Map{
		"data": string(content),
	})
}

// getBtLogs 获取回测日志
func getBtLogs(c *fiber.Ctx) error {
	type LogArgs struct {
		TaskID int64 `query:"task_id" validate:"required"`
		End    int64 `query:"end"`
		Limit  int   `query:"limit"`
	}
	var args = new(LogArgs)
	if err := base.VerifyArg(c, args, base.ArgQuery); err != nil {
		return err
	}

	btPath, err := getBtPath(args.TaskID)
	if err != nil {
		return fmt.Errorf("get backtest path failed: %v", err)
	}

	// 读取out.log
	logPath := filepath.Join(btPath, "out.log")
	file, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("open log file failed: %v", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("get file info failed: %v", err)
	}
	fileSize := fileInfo.Size()

	if args.Limit <= 0 {
		args.Limit = 1000
	}

	// 读取日志内容
	buffer := make([]byte, 4096)
	var lines []string
	var pos = fileSize
	if args.End > 0 && args.End < fileSize {
		pos = args.End
	}
	for len(lines) < args.Limit && pos > 0 {
		readSize := int64(len(buffer))
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize

		_, err = file.Seek(pos, 0)
		if err != nil {
			return fmt.Errorf("seek file failed: %v", err)
		}
		n, err := file.Read(buffer[:readSize])
		if err != nil && err != io.EOF {
			return fmt.Errorf("read file failed: %v", err)
		}

		content := string(buffer[:n])
		newLines := strings.Split(content, "\n")

		if len(lines) > 0 && len(newLines) > 0 {
			lines[0] = newLines[len(newLines)-1] + lines[0]
			newLines = newLines[:len(newLines)-1]
		}

		merge := make([]string, 0, len(lines)+len(newLines))
		merge = append(merge, newLines...)
		merge = append(merge, lines...)
		lines = merge
	}

	return c.JSON(fiber.Map{
		"data":  strings.Join(lines, "\n"),
		"start": pos,
	})
}

// getBtHtml 获取回测HTML报告
func getBtHtml(c *fiber.Ctx) error {
	type HtmlArgs struct {
		TaskID int64  `query:"task_id" validate:"required"`
		Type   string `query:"type" validate:"required"` // assets 或 enters
	}
	var args = new(HtmlArgs)
	if err := base.VerifyArg(c, args, base.ArgQuery); err != nil {
		return err
	}

	btPath, err := getBtPath(args.TaskID)
	if err != nil {
		return fmt.Errorf("get backtest path failed: %v", err)
	}

	var htmlPath string
	if args.Type == "assets" {
		htmlPath = filepath.Join(btPath, "assets.html")
	} else if args.Type == "enters" {
		htmlPath = filepath.Join(btPath, "enters.html")
	} else {
		return fmt.Errorf("invalid type: %s", args.Type)
	}

	content, err := os.ReadFile(htmlPath)
	if err != nil {
		return fmt.Errorf("read html file failed: %v", err)
	}

	c.Set("Content-Type", "text/html")
	return c.Send(content)
}
