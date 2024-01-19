package biz

import (
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/goods"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
)

func SetupComs() *errs.Error {
	log.Setup(config.Args.Debug, config.Args.Logfile)
	err := exg.Setup()
	if err != nil {
		return err
	}
	err = orm.Setup()
	if err != nil {
		return err
	}
	err = goods.Setup()
	if err != nil {
		return err
	}
	return nil
}
