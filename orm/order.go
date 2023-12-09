package orm

import (
	"github.com/anyongjin/banbot/utils"
	"strconv"
	"strings"
)

func (i *InOutOrder) SetInfo(key string, val interface{}) {
	i.Info[key] = val
}

func (i *InOutOrder) GetInfoFloat64(key string) float64 {
	val, ok := i.Info[key]
	if !ok {
		return 0
	}
	return utils.ConvertFloat64(val)
}

func (i *InOutOrder) EnterCost() float64 {
	if i.Enter.Filled == 0 {
		return 0
	}
	var price float64
	if i.Enter.Average > 0 {
		price = i.Enter.Average
	} else if i.Enter.Price > 0 {
		price = i.Enter.Price
	} else {
		price = i.Main.InitPrice
	}
	return i.Enter.Filled * price
}

func (i *InOutOrder) Key() string {
	side := "long"
	if i.Main.Short {
		side = "short"
	}
	enterAt := strconv.FormatInt(i.Main.EnterAt, 10)
	return strings.Join([]string{i.Main.Symbol, i.Main.Strategy, side, i.Main.EnterTag, enterAt}, "|")
}
