package dev

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/orm/ormo"
	"github.com/banbox/banbot/orm/ormu"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/banbox/banbot/utils"
	"github.com/banbox/banbot/web/base"
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
	api.Post("/file_op", handleFileOp)
	api.Post("/new_strat", handleNewStrat)
	api.Get("/text", getText)
	api.Post("/save_text", saveText)
	api.Post("/build", handleBuild)
	api.Get("/logs", getLogs)
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
			// 对于目录，添加末尾��斜杠
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
		// 计算本次读取的大小和位置
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

		// 将内容按行分割
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
