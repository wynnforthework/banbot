package orm

import (
	"context"
	"fmt"
	"strings"

	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg/errs"
)

type FindKHolesArgs struct {
	Sid       int32
	TimeFrame string
	Start     int64
	Stop      int64
	Limit     int
	Offset    int
}

func (q *Queries) FindKHoles(args FindKHolesArgs) ([]*KHole, int64, *errs.Error) {
	var b strings.Builder
	sqlParams := make([]interface{}, 0, 4)

	if args.Sid == 0 {
		return nil, 0, errs.NewMsg(core.ErrDbReadFail, "sid is required")
	}

	// 构建WHERE条件
	whereClause := fmt.Sprintf("where sid=$%v ", len(sqlParams)+1)
	sqlParams = append(sqlParams, args.Sid)
	if args.TimeFrame != "" {
		whereClause += fmt.Sprintf("and timeframe=$%v ", len(sqlParams)+1)
		sqlParams = append(sqlParams, args.TimeFrame)
	}
	if args.Start > 0 {
		whereClause += fmt.Sprintf("and start >= $%v ", len(sqlParams)+1)
		sqlParams = append(sqlParams, args.Start)
	}
	if args.Stop > 0 {
		whereClause += fmt.Sprintf("and stop <= $%v ", len(sqlParams)+1)
		sqlParams = append(sqlParams, args.Stop)
	}

	// 查询总数
	countQuery := "select count(*) from khole " + whereClause
	var totalNum int64
	ctx := context.Background()
	err := q.db.QueryRow(ctx, countQuery, sqlParams...).Scan(&totalNum)
	if err != nil {
		return nil, 0, NewDbErr(core.ErrDbReadFail, err)
	}

	// 查询数据
	b.WriteString("select id,sid,timeframe,start,stop,no_data from khole ")
	b.WriteString(whereClause)
	b.WriteString("order by start desc ")

	limit := 100
	if args.Limit > 0 {
		limit = args.Limit
	}
	if args.Offset > 0 {
		b.WriteString(fmt.Sprintf("offset $%v ", len(sqlParams)+1))
		sqlParams = append(sqlParams, args.Offset)
	}
	b.WriteString(fmt.Sprintf("limit $%v", len(sqlParams)+1))
	sqlParams = append(sqlParams, limit)

	rows, err := q.db.Query(ctx, b.String(), sqlParams...)
	if err != nil {
		return nil, 0, NewDbErr(core.ErrDbReadFail, err)
	}
	defer rows.Close()

	var holes []*KHole
	for rows.Next() {
		var hole KHole
		if err := rows.Scan(&hole.ID, &hole.Sid, &hole.Timeframe, &hole.Start, &hole.Stop, &hole.NoData); err != nil {
			return nil, 0, NewDbErr(core.ErrDbReadFail, err)
		}
		holes = append(holes, &hole)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, NewDbErr(core.ErrDbReadFail, err)
	}

	return holes, totalNum, nil
}
