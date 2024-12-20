package ormu

import (
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg/errs"
)

func Conn() (*Queries, *errs.Error) {
	db, err := orm.DbLite(orm.DbUI, core.DevDbPath, true)
	if err != nil {
		return nil, err
	}
	return New(db), nil
}
