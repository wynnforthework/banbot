package orm

import (
	"context"
	"fmt"
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
	symbols, err := sess.ListSymbols(ctx, ListSymbolsParams{
		Market: "binance",
	})
	if err != nil {
		fmt.Printf("list symbols fail: %s", err)
		return
	}
	fmt.Printf("loaded %d symbols", len(symbols))
}
