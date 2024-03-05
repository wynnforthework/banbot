package config

import (
	"github.com/banbox/banbot/utils"
)

func (a *CmdArgs) Init() {
	a.TimeFrames = utils.SplitSolid(a.RawTimeFrames, ",")
	a.Pairs = utils.SplitSolid(a.RawPairs, ",")
	a.Tables = utils.SplitSolid(a.RawTables, ",")
}
