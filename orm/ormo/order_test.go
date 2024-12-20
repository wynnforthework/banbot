package ormo

import (
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg/errs"
	"testing"
)

func initApp() *errs.Error {
	var args config.CmdArgs
	args.Init()
	return config.LoadConfig(&args)
}

func TestGetOrders(t *testing.T) {
	err := initApp()
	if err != nil {
		panic(err)
	}
	sess, err := Conn(orm.DbTrades, false)
	if err != nil {
		panic(err)
	}
	sess.GetOrders(GetOrdersArgs{})
}
