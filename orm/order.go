package orm

func (i *InOutOrder) SetInfo(key string, val interface{}) {
	i.Info[key] = val
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
