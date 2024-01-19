package orm

import (
	"context"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

func InitTask(sess *Queries) {
	if Task != nil {
		return
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
		return
	}
	ctx := context.Background()
	var err error
	var task *BotTask
	task, err = sess.FindTask(ctx, FindTaskParams{
		Mode: core.RunMode,
		Name: config.Name,
	})
	if err != nil || !isLiveMode {
		startAt := btime.UTCStamp()
		if !isLiveMode {
			startAt = config.TimeRange.StartMS
		}
		task, err = sess.AddTask(ctx, AddTaskParams{
			Mode:     core.RunMode,
			Name:     config.Name,
			CreateAt: btime.UTCStamp(),
			StartAt:  startAt,
			StopAt:   0,
		})
		utils.Check(err)
	}
	Task = task
	TaskID = Task.ID
	log.Info("init task ok", zap.Int64("id", TaskID))
}
