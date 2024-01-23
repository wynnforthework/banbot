package orm

import (
	"context"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	pool   *pgxpool.Pool
	TaskID int64
	Task   *BotTask
)

func Setup() *errs.Error {
	if pool != nil {
		pool.Close()
		pool = nil
	}
	dbCfg := config.Database
	if dbCfg == nil {
		return errs.NewMsg(core.ErrBadConfig, "database config is missing!")
	}
	dbPool, err := pgxpool.New(context.Background(), dbCfg.Url)
	if err != nil {
		return errs.New(core.ErrDbConnFail, err)
	}
	pool = dbPool
	row := pool.QueryRow(context.Background(), "show timezone;")
	var tz string
	err = row.Scan(&tz)
	if err != nil {
		return errs.New(core.ErrDbReadFail, err)
	}
	log.Info("connect db ok")
	return nil
}

func Conn(ctx context.Context) (*Queries, *pgxpool.Conn, *errs.Error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, errs.New(core.ErrDbConnFail, err)
	}
	return New(conn), conn, nil
}

func (q *Queries) NewTx(ctx context.Context) (pgx.Tx, *Queries, *errs.Error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, nil, errs.New(core.ErrDbConnFail, err)
	}
	nq := q.WithTx(tx)
	return tx, nq, nil
}

func (q *Queries) Exec(sql string, args ...interface{}) *errs.Error {
	_, err_ := q.db.Exec(context.Background(), sql, args...)
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	return nil
}
