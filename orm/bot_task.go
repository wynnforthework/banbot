package orm

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"strings"
)

func InitTask() *errs.Error {
	if len(accTasks) > 0 {
		return nil
	}
	if config.NoDB {
		if core.LiveMode {
			panic("`nodb` not available in live mode!")
		}
		accTasks[config.DefAcc] = &BotTask{ID: -1, Mode: core.RunMode, CreateAt: btime.UTCStamp(),
			StartAt: config.TimeRange.StartMS, StopAt: config.TimeRange.EndMS}
		taskIdAccMap[-1] = config.DefAcc
		log.Info("init task ok", zap.Int64("id", -1))
		return nil
	}
	idList := make([]string, 0, len(config.Accounts))
	for account := range config.Accounts {
		task, err := getAccTask(account)
		if err != nil {
			return err
		}
		accTasks[account] = task
		taskIdAccMap[task.ID] = account
		idList = append(idList, fmt.Sprintf("%s:%v", account, task.ID))
	}
	log.Info("init task ok", zap.String("id", strings.Join(idList, ", ")))
	return nil
}

func getAccTask(account string) (*BotTask, *errs.Error) {
	ctx := context.Background()
	var err_ error
	var err *errs.Error
	var task *BotTask
	sess, conn, err := Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	taskName := config.Name
	if core.EnvReal {
		taskName += "/" + account
	}
	task, err_ = sess.FindTask(ctx, FindTaskParams{
		Mode: core.RunMode,
		Name: config.Name,
	})
	isLiveMode := core.LiveMode
	if err_ != nil || !isLiveMode {
		startAt := btime.UTCStamp()
		if !isLiveMode {
			startAt = config.TimeRange.StartMS
		}
		task, err_ = sess.AddTask(ctx, AddTaskParams{
			Mode:     core.RunMode,
			Name:     config.Name,
			CreateAt: btime.UTCStamp(),
			StartAt:  startAt,
			StopAt:   0,
		})
		if err_ != nil {
			return nil, errs.New(core.ErrDbExecFail, err_)
		}
	}
	return task, nil
}
