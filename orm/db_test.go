package orm

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestDb(t *testing.T) {
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, "user=pqgotest dbname=pqgotest sslmode=verify-full")
	if err != nil {
		fmt.Printf("conn db fail: %s", err)
		return
	}
	defer conn.Close(ctx)
	sess := New(conn)
	symbols, err := sess.ListSymbols(ctx, "binance")
	if err != nil {
		fmt.Printf("list goods fail: %s", err)
		return
	}
	fmt.Printf("loaded %d goods", len(symbols))
}

func TestDbMigration(t *testing.T) {
	var args config.CmdArgs
	err := config.LoadConfig(&args)
	if err != nil {
		panic(err)
	}
	pool, err = pgConnPool()
	if err != nil {
		panic(err)
	}
	err = runMigrations(context.Background(), pool)
	if err != nil {
		panic(err)
	}
}
