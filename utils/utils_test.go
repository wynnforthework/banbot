package utils

import (
	"fmt"
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
