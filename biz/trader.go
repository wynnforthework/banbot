package biz

import (
	"github.com/banbox/banexg/errs"
)

type Trader struct {
	Name string
}

func (t *Trader) Init() *errs.Error {
	return SetupComs()
}
