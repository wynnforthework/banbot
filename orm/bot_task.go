package orm

import (
	"context"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

func InitTask() *errs.Error {
	if Task != nil {
		return nil
	}
	isLiveMode := core.LiveMode()
	if config.NoDB {
		if isLiveMode {
			panic("`no_db` not available in live mode!")
		}
		Task = &BotTask{ID: -1, Mode: core.RunMode, CreateAt: btime.UTCStamp(),
			StartAt: config.TimeRange.StartMS, StopAt: config.TimeRange.EndMS}
		TaskID = -1
		log.Info("init task ok", zap.Int64("id", TaskID))
		return nil
	}
	ctx := context.Background()
	var err_ error
	var task *BotTask
	sess, conn, err_ := Conn(ctx)
	if err_ != nil {
		return errs.New(core.ErrDbConnFail, err_)
	}
	defer conn.Release()
	task, err_ = sess.FindTask(ctx, FindTaskParams{
		Mode: core.RunMode,
		Name: config.Name,
	})
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
			return errs.New(core.ErrDbExecFail, err_)
		}
	}
	Task = task
	TaskID = Task.ID
	log.Info("init task ok", zap.Int64("id", TaskID))
	return nil
}
