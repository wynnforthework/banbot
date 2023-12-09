package orm

import (
	"context"
	"github.com/anyongjin/banbot/btime"
	"github.com/anyongjin/banbot/config"
	"github.com/anyongjin/banbot/log"
	"github.com/anyongjin/banbot/utils"
	"go.uber.org/zap"
)

func InitTask(sess *Queries) {
	if Task != nil {
		return
	}
	isLiveMode := config.LiveMode()
	if config.NoDB {
		if isLiveMode {
			panic("`no_db` not available in live mode!")
		}
		Task = &BotTask{ID: -1, Mode: config.RunMode, CreateAt: btime.UTCStamp(),
			StartAt: config.TimeRange.Start, StopAt: config.TimeRange.Stop}
		TaskID = -1
		log.Info("init task ok", zap.Int64("id", TaskID))
		return
	}
	ctx := context.Background()
	var err error
	var task *BotTask
	task, err = sess.FindTask(ctx, FindTaskParams{
		Mode: config.RunMode,
		Name: config.Name,
	})
	if err != nil || !isLiveMode {
		startAt := btime.UTCStamp()
		if !isLiveMode {
			startAt = config.TimeRange.Start
		}
		task, err = sess.AddTask(ctx, AddTaskParams{
			Mode:     config.RunMode,
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
