package ormu

import (
	"github.com/banbox/banexg/utils"
)

func (t *Task) ToMap() map[string]interface{} {
	taskMap := map[string]interface{}{
		"id":          t.ID,
		"mode":        t.Mode,
		"args":        t.Args,
		"config":      t.Config,
		"path":        t.Path,
		"strats":      t.Strats,
		"periods":     t.Periods,
		"pairs":       t.Pairs,
		"createAt":    t.CreateAt,
		"startAt":     t.StartAt,
		"stopAt":      t.StopAt,
		"status":      t.Status,
		"progress":    t.Progress,
		"orderNum":    t.OrderNum,
		"profitRate":  t.ProfitRate,
		"winRate":     t.WinRate,
		"maxDrawdown": t.MaxDrawdown,
		"sharpe":      t.Sharpe,
		"note":        t.Note,
	}

	if t.Info != "" {
		var infoMap map[string]interface{}
		if err := utils.Unmarshal([]byte(t.Info), &infoMap, utils.JsonNumAuto); err == nil {
			for k, v := range infoMap {
				taskMap[k] = v
			}
		}
	}
	return taskMap
}
