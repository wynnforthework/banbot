package orm

import (
	"context"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/bytedance/sonic"
	"strconv"
	"strings"
)

func (i *InOutOrder) SetInfo(key string, val interface{}) {
	if val == nil {
		delete(i.Info, key)
	} else {
		i.Info[key] = val
	}
	i.DirtyInfo = true
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
		price = i.InitPrice
	}
	return i.Enter.Filled * price
}

func (i *InOutOrder) Key() string {
	side := "long"
	if i.Short {
		side = "short"
	}
	enterAt := strconv.FormatInt(i.EnterAt, 10)
	return strings.Join([]string{i.Symbol, i.Strategy, side, i.EnterTag, enterAt}, "|")
}

/*
CalcProfit
返回利润（未扣除手续费）
*/
func (i *InOutOrder) CalcProfit(price float64) float64 {
	if i.Status == InOutStatusInit || i.Enter == nil || i.Enter.Average == 0 || i.Enter.Filled == 0 {
		return 0
	}
	if price == 0 {
		if i.Exit != nil {
			if i.Exit.Average > 0 {
				price = i.Exit.Average
			} else if i.Exit.Price > 0 {
				price = i.Exit.Price
			}
		}
		if price == 0 && i.Enter.Price > 0 {
			price = i.Enter.Price
		}
	}
	entQuoteVal := i.Enter.Average * i.Enter.Filled
	profitVal := i.Enter.Filled*price - entQuoteVal
	if i.Short {
		profitVal = 0 - profitVal
	}
	return profitVal
}

func (i *InOutOrder) UpdateProfits(price float64) {
	profitVal := i.CalcProfit(price)
	if profitVal == 0 {
		return
	}
	enterFee, exitFee := float64(0), float64(0)
	if i.Enter != nil {
		enterFee = i.Enter.Fee
	}
	if i.Exit != nil {
		exitFee = i.Exit.Fee
	}
	i.Profit = profitVal - enterFee - exitFee
	entPrice := i.InitPrice
	if i.Enter.Average > 0 {
		entPrice = i.Enter.Average
	} else if i.Enter.Price > 0 {
		entPrice = i.Enter.Price
	}
	entQuoteVal := entPrice * i.Enter.Filled
	if i.Leverage > 0 {
		entQuoteVal /= float64(i.Leverage)
	}
	i.ProfitRate = i.Profit / entQuoteVal
	i.DirtyMain = true
}

func (i *InOutOrder) CanClose() bool {
	if i.ExitTag != "" {
		return false
	}
	if i.Timeframe == "ws" {
		return true
	}
	tfMSecs := int64(utils.TFToSecs(i.Timeframe) * 1000)
	return float64(btime.TimeMS()-i.EnterAt) > float64(tfMSecs)*0.9
}

func (i *InOutOrder) SetExit(tag, orderType string, limit float64) {
	if i.ExitAt == 0 {
		if tag == "" {
			tag = core.ExitTagUnknown
		}
		i.ExitTag = tag
		i.ExitAt = btime.TimeMS()
		i.DirtyMain = true
	}
	if i.Exit == nil {
		odSide := banexg.OdSideSell
		if i.Short {
			odSide = banexg.OdSideBuy
		}
		i.Exit = &ExOrder{
			TaskID:    i.TaskID,
			InoutID:   i.ID,
			Symbol:    i.Symbol,
			Enter:     false,
			OrderType: orderType,
			Side:      odSide,
			CreateAt:  btime.TimeMS(),
			Price:     limit,
			Amount:    i.Enter.Amount,
			Status:    OdStatusInit,
		}
		i.DirtyExit = true
	} else {
		if orderType != "" {
			i.Exit.OrderType = orderType
			i.DirtyExit = true
		}
		if limit > 0 {
			i.Exit.Price = limit
			i.DirtyExit = true
		}
	}
}

/*
LocalExit
在本地强制退出订单，立刻生效，无需等到下一个bar。这里不涉及钱包更新，钱包需要自行更新。
*/
func (i *InOutOrder) LocalExit(tag string, price float64, msg string) {
	if price == 0 {
		newPrice := core.GetPrice(i.Symbol)
		if newPrice > 0 {
			price = newPrice
		} else if i.Enter.Average > 0 {
			price = i.Enter.Average
		} else if i.Enter.Price > 0 {
			price = i.Enter.Price
		} else {
			price = i.InitPrice
		}
	}
	i.SetExit(tag, banexg.OdTypeMarket, price)
	if i.Enter.Status < OdStatusClosed {
		i.Enter.Status = OdStatusClosed
		i.DirtyEnter = true
	}
	i.Exit.Status = OdStatusClosed
	i.Exit.Filled = i.Enter.Filled
	i.Exit.Average = i.Exit.Price
	i.Status = InOutStatusFullExit
	i.UpdateProfits(price)
	i.DirtyMain = true
	i.DirtyExit = true
	if msg != "" {
		i.SetInfo(KeyStatusMsg, msg)
	}
}

/*
ForceExit
强制退出订单，如已买入，则以市价单退出。如买入未成交，则取消挂单，如尚未提交，则直接删除订单

	生成模式：提交请求到交易所。
	模拟模式：在下一个bar出现后完成退出
*/
func (i *InOutOrder) ForceExit(tag string, msg string) {
	panic("ForceExit not implement")
}

/*
CutPart
从当前订单分割出一个小的InOutOrder，解决一次买入，多次卖出问题
*/
func (i *InOutOrder) CutPart(enterAmt, exitAmt float64) *InOutOrder {
	enterRate := enterAmt / i.Enter.Amount
	exitRate := float64(0)
	if i.Exit != nil && exitAmt > 0 {
		exitRate = exitAmt / i.Exit.Amount
	}
	part := &InOutOrder{
		IOrder: &IOrder{
			TaskID:    i.TaskID,
			Symbol:    i.Symbol,
			Sid:       i.Sid,
			Timeframe: i.Timeframe,
			Short:     i.Short,
			Status:    i.Status,
			EnterTag:  i.EnterTag,
			InitPrice: i.InitPrice,
			QuoteCost: i.QuoteCost * enterRate,
			Leverage:  i.Leverage,
			EnterAt:   i.EnterAt,
			Strategy:  i.Strategy,
			StgVer:    i.StgVer,
			Info:      i.IOrder.Info,
		},
	}
	// 原来订单的enter_at需要+1，防止和拆分的子订单冲突。
	i.EnterAt += 1
	i.QuoteCost -= part.QuoteCost
	partEnter := part.Enter.CutPart(enterRate, true)
	partEnter.InoutID = part.ID
	part.Enter = partEnter
	if exitRate == 0 && i.Exit != nil && i.Exit.Amount > i.Enter.Amount {
		exitRate = (i.Exit.Amount - i.Enter.Amount) / i.Exit.Amount
	}
	if exitRate > 0 && i.Exit != nil {
		part.ExitAt = i.ExitAt
		part.ExitTag = i.ExitTag
		partExit := i.Exit.CutPart(exitRate, true)
		partExit.InoutID = part.ID
		part.Exit = partExit
	}
	return part
}

func (i *InOutOrder) Save(sess *Queries) *errs.Error {
	if core.LiveMode() {
		err := i.saveToDb(sess)
		if err != nil {
			return err
		}
		if i.Status < InOutStatusFullExit {
			OpenODs[i.ID] = i
		} else {
			delete(OpenODs, i.ID)
		}
	} else {
		i.saveToMem()
	}
	return nil
}

func (i *InOutOrder) saveToMem() {
	if i.ID == 0 {
		i.ID = FakeOdId
		FakeOdId += 1
	}
	if i.Status < InOutStatusFullExit {
		OpenODs[i.ID] = i
	} else {
		if _, ok := OpenODs[i.ID]; ok {
			delete(OpenODs, i.ID)
			HistODs = append(HistODs, i)
		}
	}
}

func (i *InOutOrder) saveToDb(sess *Queries) *errs.Error {
	var err *errs.Error
	if i.DirtyInfo {
		i.IOrder.Info, err = i.GetInfoText()
		if err != nil {
			return err
		}
		i.DirtyInfo = false
	}
	if i.ID == 0 {
		err = i.IOrder.saveAdd(sess)
		if err != nil {
			return err
		}
		i.DirtyMain = false
		i.Enter.InoutID = i.ID
		i.Exit.InoutID = i.ID
		err = i.Enter.saveAdd(sess)
		if err != nil {
			return err
		}
		i.DirtyEnter = false
		if i.Exit != nil && i.Exit.Symbol != "" {
			err = i.Exit.saveAdd(sess)
			if err != nil {
				return err
			}
		}
		i.DirtyExit = false
	} else {
		if i.DirtyMain {
			err = i.IOrder.saveUpdate(sess)
			if err != nil {
				return err
			}
			i.DirtyMain = false
		}
		if i.DirtyEnter {
			err = i.Enter.saveUpdate(sess)
			if err != nil {
				return err
			}
			i.DirtyEnter = false
		}
		if i.DirtyExit && i.Exit != nil && i.Exit.Symbol != "" {
			err = i.Exit.saveUpdate(sess)
			if err != nil {
				return err
			}
			i.DirtyExit = false
		}
	}
	return nil
}

func (i *InOutOrder) GetInfoText() (string, *errs.Error) {
	if i.Info != nil && len(i.Info) > 0 {
		infoText, err_ := sonic.MarshalString(i.Info)
		if err_ != nil {
			return "", errs.New(errs.CodeUnmarshalFail, err_)
		}
		return infoText, nil
	}
	return "", nil
}

func (i *IOrder) saveAdd(sess *Queries) *errs.Error {
	var err_ error
	i.ID, err_ = sess.AddIOrder(context.Background(), AddIOrderParams{
		TaskID:     int32(i.TaskID),
		Symbol:     i.Symbol,
		Sid:        int64(i.Sid),
		Timeframe:  i.Timeframe,
		Short:      i.Short,
		Status:     i.Status,
		EnterTag:   i.EnterTag,
		InitPrice:  i.InitPrice,
		QuoteCost:  i.QuoteCost,
		ExitTag:    i.ExitTag,
		Leverage:   i.Leverage,
		EnterAt:    i.EnterAt,
		ExitAt:     i.ExitAt,
		Strategy:   i.Strategy,
		StgVer:     i.StgVer,
		ProfitRate: i.ProfitRate,
		Profit:     i.Profit,
		Info:       i.Info,
	})
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	return nil
}

func (i *IOrder) saveUpdate(sess *Queries) *errs.Error {
	err_ := sess.SetIOrder(context.Background(), SetIOrderParams{
		TaskID:     int32(i.TaskID),
		Symbol:     i.Symbol,
		Sid:        int64(i.Sid),
		Timeframe:  i.Timeframe,
		Short:      i.Short,
		Status:     i.Status,
		EnterTag:   i.EnterTag,
		InitPrice:  i.InitPrice,
		QuoteCost:  i.QuoteCost,
		ExitTag:    i.ExitTag,
		Leverage:   i.Leverage,
		EnterAt:    i.EnterAt,
		ExitAt:     i.ExitAt,
		Strategy:   i.Strategy,
		StgVer:     i.StgVer,
		ProfitRate: i.ProfitRate,
		Profit:     i.Profit,
		Info:       i.Info,
		ID:         i.ID,
	})
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	return nil
}

func (i *ExOrder) saveAdd(sess *Queries) *errs.Error {
	var err_ error
	i.ID, err_ = sess.AddExOrder(context.Background(), AddExOrderParams{
		TaskID:    int32(i.TaskID),
		InoutID:   int32(i.InoutID),
		Symbol:    i.Symbol,
		Enter:     i.Enter,
		OrderType: i.OrderType,
		OrderID:   i.OrderID,
		Side:      i.Side,
		CreateAt:  i.CreateAt,
		Price:     i.Price,
		Average:   i.Average,
		Amount:    i.Amount,
		Filled:    i.Filled,
		Status:    i.Status,
		Fee:       i.Fee,
		FeeType:   i.FeeType,
		UpdateAt:  i.UpdateAt,
	})
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	return nil
}

func (i *ExOrder) saveUpdate(sess *Queries) *errs.Error {
	err_ := sess.SetExOrder(context.Background(), SetExOrderParams{
		TaskID:    int32(i.TaskID),
		InoutID:   int32(i.InoutID),
		Symbol:    i.Symbol,
		Enter:     i.Enter,
		OrderType: i.OrderType,
		OrderID:   i.OrderID,
		Side:      i.Side,
		CreateAt:  i.CreateAt,
		Price:     i.Price,
		Average:   i.Average,
		Amount:    i.Amount,
		Filled:    i.Filled,
		Status:    i.Status,
		Fee:       i.Fee,
		FeeType:   i.FeeType,
		UpdateAt:  i.UpdateAt,
		ID:        i.ID,
	})
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	return nil
}

func (i *ExOrder) CutPart(rate float64, fill bool) *ExOrder {
	part := &ExOrder{
		TaskID:    i.TaskID,
		Symbol:    i.Symbol,
		Enter:     i.Enter,
		OrderType: i.OrderType,
		OrderID:   i.OrderID,
		Side:      i.Side,
		CreateAt:  i.CreateAt,
		Price:     i.Price,
		Average:   i.Average,
		Amount:    i.Amount * rate,
		Fee:       i.Fee,
		FeeType:   i.FeeType,
		UpdateAt:  i.UpdateAt,
	}
	i.Amount -= part.Amount
	if fill && i.Filled > 0 {
		if i.Filled <= part.Amount {
			part.Filled = i.Filled
			i.Filled = 0
		} else {
			part.Filled = part.Amount
			i.Filled -= part.Filled
		}
	} else if i.Filled > i.Amount {
		part.Filled = i.Filled - i.Amount
		i.Filled = i.Amount
	}
	if part.Filled >= part.Amount {
		part.Status = OdStatusClosed
	} else if part.Filled > 0 {
		part.Status = OdStatusPartOK
	} else {
		part.Status = OdStatusInit
	}
	if i.Filled >= i.Amount {
		i.Status = OdStatusClosed
	}
	return part
}

func (q *Queries) DumpOrdersToDb() *errs.Error {
	allOrders := append(HistODs, utils.ValsOfMap(OpenODs)...)
	for _, od := range allOrders {
		od.ID = 0
		err := od.saveToDb(q)
		if err != nil {
			return err
		}
	}
	return nil
}
