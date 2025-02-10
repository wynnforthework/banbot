package ormu

import (
	"context"
	"fmt"
	"strings"

	"github.com/banbox/banbot/core"

	"github.com/banbox/banexg/errs"
)

type FindTasksParams struct {
	Mode     string `json:"mode"`     // 可选的模式筛选
	Path     string `json:"path"`     // 可选的路径筛选
	Status   int64  `json:"status"`   // 可选的状态筛选
	Strat    string `json:"strat"`    // 可选的策略筛选（模糊匹配）
	Period   string `json:"period"`   // 可选的周期筛选（模糊匹配）
	StartAt  int64  `json:"startAt"`  // 可选的开始时间精确匹配
	StopAt   int64  `json:"stopAt"`   // 可选的结束时间精确匹配
	MinStart int64  `json:"minStart"` // 可选的最小开始时间
	MaxStart int64  `json:"maxStart"` // 可选的最大开始时间
	MaxID    int64  `json:"maxId"`    // 可选的最大ID筛选
	Limit    int64  `json:"limit"`    // 限制返回数量
}

func (q *Queries) FindTasks(ctx context.Context, arg FindTasksParams) ([]*Task, *errs.Error) {
	var b strings.Builder
	b.WriteString(`SELECT id, mode, args, config, path, strats, periods, pairs, 
		create_at, start_at, stop_at, status, progress, order_num, profit_rate, win_rate, max_drawdown, sharpe, info, note 
		FROM task WHERE 1=1 `)

	params := make([]interface{}, 0)
	paramCount := 1

	// 添加可选的筛选条件
	if arg.Mode != "" {
		b.WriteString(fmt.Sprintf("AND mode = $%d ", paramCount))
		params = append(params, arg.Mode)
		paramCount++
	}

	if arg.Path != "" {
		b.WriteString(fmt.Sprintf("AND path = $%d ", paramCount))
		params = append(params, arg.Path)
		paramCount++
	}

	if arg.Status > 0 {
		b.WriteString(fmt.Sprintf("AND status = $%d ", paramCount))
		params = append(params, arg.Status)
		paramCount++
	}

	if arg.Strat != "" {
		b.WriteString(fmt.Sprintf("AND strats LIKE $%d ", paramCount))
		params = append(params, "%"+arg.Strat+"%")
		paramCount++
	}

	if arg.Period != "" {
		b.WriteString(fmt.Sprintf("AND periods LIKE $%d ", paramCount))
		params = append(params, "%"+arg.Period+"%")
		paramCount++
	}

	if arg.StartAt > 0 {
		b.WriteString(fmt.Sprintf("AND start_at = $%d ", paramCount))
		params = append(params, arg.StartAt)
		paramCount++
	}

	if arg.StopAt > 0 {
		b.WriteString(fmt.Sprintf("AND stop_at = $%d ", paramCount))
		params = append(params, arg.StopAt)
		paramCount++
	}

	if arg.MinStart > 0 {
		b.WriteString(fmt.Sprintf("AND start_at >= $%d ", paramCount))
		params = append(params, arg.MinStart)
		paramCount++
	}

	if arg.MaxStart > 0 {
		b.WriteString(fmt.Sprintf("AND start_at <= $%d ", paramCount))
		params = append(params, arg.MaxStart)
		paramCount++
	}

	if arg.MaxID > 0 {
		b.WriteString(fmt.Sprintf("AND id < $%d ", paramCount))
		params = append(params, arg.MaxID)
		paramCount++
	}

	b.WriteString("ORDER BY id DESC ")

	if arg.Limit > 0 {
		b.WriteString(fmt.Sprintf("LIMIT $%d", paramCount))
		params = append(params, arg.Limit)
	}

	// 执行查询
	rows, err := q.db.QueryContext(ctx, b.String(), params...)
	if err != nil {
		return nil, errs.New(core.ErrDbReadFail, err)
	}
	defer rows.Close()

	var items []*Task
	for rows.Next() {
		var i Task
		if err = rows.Scan(
			&i.ID,
			&i.Mode,
			&i.Args,
			&i.Config,
			&i.Path,
			&i.Strats,
			&i.Periods,
			&i.Pairs,
			&i.CreateAt,
			&i.StartAt,
			&i.StopAt,
			&i.Status,
			&i.Progress,
			&i.OrderNum,
			&i.ProfitRate,
			&i.WinRate,
			&i.MaxDrawdown,
			&i.Sharpe,
			&i.Info,
			&i.Note,
		); err != nil {
			return nil, errs.New(core.ErrDbReadFail, err)
		}
		items = append(items, &i)
	}

	if err = rows.Close(); err != nil {
		return nil, errs.New(core.ErrDbReadFail, err)
	}
	if err = rows.Err(); err != nil {
		return nil, errs.New(core.ErrDbReadFail, err)
	}

	return items, nil
}
