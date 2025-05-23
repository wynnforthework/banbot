package goods

import "testing"

func TestBlockFilter(t *testing.T) {
	f := BlockFilter{
		BaseFilter: BaseFilter{
			Name: "BlockFilter",
		},
		Pairs: []string{"BTC/USDT:USDT"},
	}
	src := []string{"BTC/USDT:USDT", "ETH/USDT:USDT"}
	out, err := f.Filter(src, 0)
	if err != nil {
		panic(err)
	}
	if len(out) != 1 || out[0] != "ETH/USDT:USDT" {
		t.Errorf("FAIL BlockFilter, get: %v, expect: %v", out, []string{"ETH/USDT:USDT"})
	}
}
