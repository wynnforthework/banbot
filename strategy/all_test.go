package strategy

import (
	"fmt"
	testcom "github.com/anyongjin/banbot/_testcom"
	"github.com/anyongjin/banbot/orm"
	"github.com/anyongjin/banbot/utils"
	ta "github.com/anyongjin/banta"
	"testing"
)

var env = &ta.BarEnv{
	TimeFrame:  "1d",
	TFMSecs:    86400000,
	Exchange:   "binance",
	MarketType: "future",
	BarNum:     1,
}

func TestStagyJob_DrawDownExit(t *testing.T) {
	job := &StagyJob{
		Env: env,
		Stagy: &TradeStagy{
			GetDrawDownExitRate: func(s *StagyJob, od *orm.InOutOrder, maxChg float64) float64 {
				return 0.5
			},
		},
		TPMaxs: make(map[int64]float64),
	}
	var od *orm.InOutOrder
	testcom.RunFakeEnv(env, func(i int, bar ta.Kline) {
		if i == 2 {
			od = &orm.InOutOrder{
				Main: &orm.IOrder{
					ID:        1,
					Short:     false,
					InitPrice: bar.Close,
				},
				Enter: &orm.ExOrder{
					Average: bar.Close,
				},
			}
			fmt.Printf("open long: %f curPrice: %f\n", bar.Close, bar.Close)
		} else if i == 10 {
			od = &orm.InOutOrder{
				Main: &orm.IOrder{
					ID:        2,
					Short:     true,
					InitPrice: bar.Close,
				},
				Enter: &orm.ExOrder{
					Average: bar.Close,
				},
			}
			fmt.Printf("open short: %f curPrice: %f\n", bar.Close, bar.Close)
		} else if od != nil {
			ddPrice := job.getDrawDownExitPrice(od)
			if i == 6 {
				if !utils.EqualNearly(ddPrice, 31358.5) {
					t.Errorf("FAIL long tpPrice :%f close: %f high: %f\n", ddPrice, bar.Close, bar.High)
				} else {
					t.Logf("pass long tpPrice :%f close: %f high: %f\n", ddPrice, bar.Close, bar.High)
				}
			} else if i == 17 {
				if !utils.EqualNearly(ddPrice, 30004.2) {
					t.Errorf("FAIL short tpPrice :%f close: %f low: %f\n", ddPrice, bar.Close, bar.Low)
				} else {
					t.Logf("pass short tpPrice :%f close: %f low: %f\n", ddPrice, bar.Close, bar.Low)
				}
			}
		}
	})
}

func TestStagyLoad(t *testing.T) {
	stgy := load("hammer")
	if stgy == nil {
		return
	}
	t.Logf("%s %d %d", stgy.Name, stgy.Version, stgy.WarmupNum)
}
