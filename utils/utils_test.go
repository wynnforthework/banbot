package utils

import (
	"fmt"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"testing"
)

func TestGcdInts(t *testing.T) {
	items := []struct {
		Nums []int
		Res  int
	}{
		{Nums: []int{10, 15, 30}, Res: 5},
		{Nums: []int{15, 30, 60}, Res: 15},
		{Nums: []int{9, 15, 60}, Res: 3},
	}
	for _, c := range items {
		res := GcdInts(c.Nums)
		if res != c.Res {
			t.Error(fmt.Sprintf("fail %v, exp: %d, res: %d", c.Nums, c.Res, res))
		}
	}
}

func TestCopyDir(t *testing.T) {
	name := "demo"
	stagyDir := "E:\\trade\\go\\banstrat"
	outDir := "E:\\trade\\go\\bandata\\backtest\\task_-1"
	srcDir := fmt.Sprintf("%s/%s", stagyDir, name)
	tgtDir := fmt.Sprintf("%s/strat_%s", outDir, name)
	err_ := CopyDir(srcDir, tgtDir)
	if err_ != nil {
		log.Error("backup strat fail", zap.String("name", name), zap.Error(err_))
	}
}
