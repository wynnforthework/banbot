package utils

import (
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg/errs"
)

func Setup() *errs.Error {
	core.SysLang = GetSystemLanguage()
	return nil
}
