package ormo

import (
	"path/filepath"
	"testing"

	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg/errs"
)

func initApp() *errs.Error {
	var args config.CmdArgs
	return config.LoadConfig(&args)
}

func TestGetOrders(t *testing.T) {
	err := initApp()
	if err != nil {
		panic(err)
	}
	orm.SetDbPath(orm.DbTrades, filepath.Join(config.GetDataDir(), "temp.db"))
	sess, conn, err := Conn(orm.DbTrades, false)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	sess.GetOrders(GetOrdersArgs{})
}
