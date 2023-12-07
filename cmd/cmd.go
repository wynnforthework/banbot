package cmd

import (
	"github.com/anyongjin/gobanbot/utils"
)

func (this *CmdArgs) Init() {
	this.TimeFrames = utils.SplitSolid(this.RawTimeFrames, ",")
	this.Pairs = utils.SplitSolid(this.RawPairs, ",")
	this.Tables = utils.SplitSolid(this.RawTables, ",")
}
