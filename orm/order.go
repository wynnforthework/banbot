package orm

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	"github.com/bytedance/sonic"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"math"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	iOrderFields  = "id, task_id, symbol, sid, timeframe, short, status, enter_tag, init_price, quote_cost, exit_tag, leverage, enter_at, exit_at, strategy, stg_ver, max_draw_down, profit_rate, profit, info"
	exOrderFields = "id, task_id, inout_id, symbol, enter, order_type, order_id, side, create_at, price, average, amount, filled, status, fee, fee_type, update_at"
)

func (i *InOutOrder) SetInfo(key string, val interface{}) {
	i.loadInfo()
	if val == nil {
		delete(i.Info, key)
	} else {
		oldVal, _ := i.Info[key]
		if val == oldVal {
			// 无需修改
			return
		}
		i.Info[key] = val
	}
	i.DirtyInfo = true
}

func (i *InOutOrder) loadInfo() {
	if i.Info != nil {
		return
	}
	i.Info = make(map[string]interface{})
	if i.IOrder.Info == "" {
		return
	}
	err_ := utils2.UnmarshalString(i.IOrder.Info, &i.Info)
	if err_ != nil {
		log.Error("unmarshal ioder info fail", zap.String("info", i.IOrder.Info), zap.Error(err_))
	}
}

func (i *InOutOrder) GetInfoFloat64(key string) float64 {
	i.loadInfo()
	val, ok := i.Info[key]
	if !ok {
		return 0
	}
	return utils.ConvertFloat64(val)
}

func (i *InOutOrder) GetInfoInt64(key string) int64 {
	i.loadInfo()
	val, ok := i.Info[key]
	if !ok {
		return 0
	}
	return utils.ConvertInt64(val)
}

func (i *InOutOrder) GetInfoBool(key string) bool {
	i.loadInfo()
	val, ok := i.Info[key]
	if !ok {
		return false
	}
	resVal, _ := val.(bool)
	return resVal
}

func (i *InOutOrder) GetInfoString(key string) string {
	i.loadInfo()
	return utils2.GetMapVal(i.Info, key, "")
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
	if i.idKey != "" {
		return i.idKey
	}
	side := "long"
	if i.Short {
		side = "short"
	}
	enterAt := strconv.FormatInt(i.EnterAt, 10)
	i.idKey = strings.Join([]string{i.Symbol, i.Strategy, side, i.EnterTag, enterAt}, "|")
	return i.idKey
}

func (i *InOutOrder) SetEnterLimit(price float64) *errs.Error {
	if i.Status >= InOutStatusFullEnter {
		return errs.NewMsg(errs.CodeRunTime, "cannot set entry limit for entered order: %s", i.Key())
	}
	if i.Status == InOutStatusInit {
		i.InitPrice = price
		i.DirtyMain = true
	}
	i.Enter.Price = price
	i.DirtyEnter = true
	return nil
}

/*
CalcProfit
返回利润（未扣除手续费）
*/
func (i *InOutOrder) CalcProfit(price float64) float64 {
	if i.Status == InOutStatusInit || i.Enter == nil || i.Enter.Average == 0 || i.Enter.Filled == 0 {
		return math.MaxFloat64
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
	profitVal := i.Enter.Filled * (price - i.Enter.Average)
	if i.Short {
		profitVal = 0 - profitVal
	}
	return profitVal
}

func (i *InOutOrder) UpdateProfits(price float64) {
	profitVal := i.CalcProfit(price)
	if profitVal == math.MaxFloat64 {
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
		entQuoteVal /= i.Leverage
	}
	i.ProfitRate = i.Profit / entQuoteVal
	if i.ProfitRate < 0 {
		absVal := math.Abs(i.ProfitRate)
		if absVal > i.MaxDrawDown {
			i.MaxDrawDown = absVal
		}
	}
	i.DirtyMain = true
}

/*
UpdateFee
为入场/出场订单计算手续费，必须在Filled赋值后调用，否则计算为空
*/
func (i *InOutOrder) UpdateFee(price float64, forEnter bool, isHistory bool) *errs.Error {
	exchange := exg.Default
	exOrder := i.Enter
	if !forEnter {
		exOrder = i.Exit
	}
	var maker = false
	if exOrder.OrderType != banexg.OdTypeMarket {
		if isHistory {
			// 历史已完成订单，不使用当前价格判断是否为maker，直接认为maker
			maker = true
		} else {
			maker = core.IsMaker(i.Symbol, exOrder.Side, price)
		}
	}
	fee, err := exchange.CalculateFee(i.Symbol, exOrder.OrderType, exOrder.Side, exOrder.Filled, price, maker, nil)
	if err != nil {
		return err
	}
	exOrder.Fee = fee.Cost
	exOrder.FeeType = fee.Currency
	if forEnter {
		i.DirtyEnter = true
	} else {
		i.DirtyExit = true
	}
	return nil
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
实盘时调用此函数会保存到数据库
*/
func (i *InOutOrder) LocalExit(tag string, price float64, msg, odType string) *errs.Error {
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
	if odType == "" {
		odType = banexg.OdTypeMarket
	}
	i.SetExit(tag, odType, price)
	if i.Enter.Status < OdStatusClosed {
		i.Enter.Status = OdStatusClosed
		err := i.UpdateFee(price, true, true)
		if err != nil {
			return err
		}
		i.DirtyEnter = true
	}
	i.Exit.Status = OdStatusClosed
	if i.Exit.Filled == 0 {
		i.ExitAt = btime.TimeMS()
	}
	i.Exit.Filled = i.Enter.Filled
	i.Exit.Average = i.Exit.Price
	i.Status = InOutStatusFullExit
	err := i.UpdateFee(price, false, true)
	if err != nil {
		return err
	}
	i.UpdateProfits(price)
	i.DirtyMain = true
	i.DirtyExit = true
	if msg != "" {
		i.SetInfo(KeyStatusMsg, msg)
	}
	return i.Save(nil)
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
		Info:       make(map[string]interface{}),
		DirtyMain:  true,
		DirtyEnter: true,
		DirtyInfo:  true,
	}
	for key, val := range i.Info {
		part.Info[key] = val
	}
	// 原来订单的enter_at需要+1，防止和拆分的子订单冲突。
	i.EnterAt += 1
	i.QuoteCost -= part.QuoteCost
	i.DirtyMain = true
	i.DirtyEnter = true
	partEnter := i.Enter.CutPart(enterRate, true)
	partEnter.InoutID = part.ID
	part.Enter = partEnter
	if exitRate == 0 && i.Exit != nil && i.Exit.Amount > i.Enter.Amount {
		exitRate = (i.Exit.Amount - i.Enter.Amount) / i.Exit.Amount
	}
	if exitRate > 0 && i.Exit != nil {
		i.DirtyExit = true
		part.DirtyExit = true
		part.ExitAt = i.ExitAt
		part.ExitTag = i.ExitTag
		partExit := i.Exit.CutPart(exitRate, true)
		partExit.InoutID = part.ID
		part.Exit = partExit
		if part.Status < InOutStatusFullEnter {
			part.Status = InOutStatusFullEnter
		}
	} else if part.Status > InOutStatusFullEnter {
		// 无Exit，状态不应该为退出
		part.Status = InOutStatusFullEnter
	}
	return part
}

func (i *InOutOrder) IsDirty() bool {
	return i.DirtyExit || i.DirtyMain || i.DirtyEnter || i.DirtyInfo
}

func (i *InOutOrder) Save(sess *Queries) *errs.Error {
	if i.Status == InOutStatusDelete {
		return nil
	}
	if core.LiveMode {
		err := i.saveToDb(sess)
		if err != nil {
			return err
		}
		openOds, lock := GetOpenODs(GetTaskAcc(i.TaskID))
		lock.Lock()
		if i.Status < InOutStatusFullExit {
			openOds[i.ID] = i
		} else {
			delete(openOds, i.ID)
		}
		lock.Unlock()
		mLockOds.Lock()
		delete(lockOds, i.Key())
		mLockOds.Unlock()
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
	openOds, lock := GetOpenODs(GetTaskAcc(i.TaskID))
	lock.Lock()
	if i.Status < InOutStatusFullExit {
		openOds[i.ID] = i
	} else {
		if _, ok := openOds[i.ID]; ok {
			delete(openOds, i.ID)
			HistODs = append(HistODs, i)
		}
	}
	lock.Unlock()
}

func (i *InOutOrder) saveToDb(sess *Queries) *errs.Error {
	if i.Status == InOutStatusDelete {
		return nil
	}
	var err *errs.Error
	i.IOrder.Info, err = i.GetInfoText()
	if err != nil {
		return err
	}
	if sess == nil {
		var conn *pgxpool.Conn
		sess, conn, err = Conn(nil)
		if err != nil {
			return err
		}
		defer conn.Release()
	}
	if i.ID == 0 {
		err = i.IOrder.saveAdd(sess)
		if err != nil {
			return err
		}
		i.DirtyMain = false
		i.Enter.InoutID = i.ID
		err = i.Enter.saveAdd(sess)
		if err != nil {
			return err
		}
		i.DirtyEnter = false
		if i.Exit != nil && i.Exit.Symbol != "" {
			i.Exit.InoutID = i.ID
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
			if i.Exit.ID == 0 {
				err = i.Exit.saveAdd(sess)
			} else {
				err = i.Exit.saveUpdate(sess)
			}
			if err != nil {
				return err
			}
			i.DirtyExit = false
		}
	}
	return nil
}

func (i *InOutOrder) GetInfoText() (string, *errs.Error) {
	if !i.DirtyInfo {
		return i.IOrder.Info, nil
	}
	i.DirtyInfo = false
	if i.Info != nil && len(i.Info) > 0 {
		infoText, err_ := sonic.MarshalString(i.Info)
		if err_ != nil {
			return "", errs.New(errs.CodeUnmarshalFail, err_)
		}
		i.DirtyMain = true
		return infoText, nil
	}
	return "", nil
}

/*
ClientId
生成交易所的ClientOrderId
*/
func (i *InOutOrder) ClientId(random bool) string {
	if random {
		return fmt.Sprintf("%s_%v_%v", config.Name, i.ID, rand.Intn(1000))
	}
	return fmt.Sprintf("%s_%v", config.Name, i.ID)
}

type InOutSnap struct {
	EnterLimit      float64
	ExitLimit       float64
	StopLoss        float64
	TakeProfit      float64
	StopLossLimit   float64
	TakeProfitLimit float64
}

func (i *InOutOrder) TakeSnap() *InOutSnap {
	snap := &InOutSnap{}
	if i.Status < InOutStatusFullEnter && i.Enter != nil && i.Enter.Status < OdStatusClosed {
		snap.EnterLimit = i.Enter.Price
	}
	if i.Status > InOutStatusFullEnter && i.Exit != nil && i.Exit.Status < OdStatusClosed {
		snap.ExitLimit = i.Exit.Price
	}
	snap.StopLoss = i.GetInfoFloat64(OdInfoStopLoss)
	snap.TakeProfit = i.GetInfoFloat64(OdInfoTakeProfit)
	snap.StopLossLimit = i.GetInfoFloat64(OdInfoStopLossLimit)
	snap.TakeProfitLimit = i.GetInfoFloat64(OdInfoTakeProfitLimit)
	return snap
}

/*
返回修改当前订单的锁，返回成功表示已获取锁
*/
func (i *InOutOrder) Lock() *sync.Mutex {
	odKey := i.Key()
	mLockOds.Lock()
	defer mLockOds.Unlock()
	lock, ok := lockOds[odKey]
	if !ok {
		lock = &sync.Mutex{}
		lockOds[odKey] = lock
	}
	var got = false
	if core.LiveMode {
		// 实时模式，增加死锁检测
		time.AfterFunc(time.Second*5, func() {
			if got {
				return
			}
			log.Error("order DeadLock found", zap.String("key", odKey))
		})
	}
	lock.Lock()
	got = true
	return lock
}

func (i *IOrder) saveAdd(sess *Queries) *errs.Error {
	var err_ error
	i.ID, err_ = sess.AddIOrder(context.Background(), AddIOrderParams{
		TaskID:      int32(i.TaskID),
		Symbol:      i.Symbol,
		Sid:         i.Sid,
		Timeframe:   i.Timeframe,
		Short:       i.Short,
		Status:      i.Status,
		EnterTag:    i.EnterTag,
		InitPrice:   i.InitPrice,
		QuoteCost:   i.QuoteCost,
		ExitTag:     i.ExitTag,
		Leverage:    i.Leverage,
		EnterAt:     i.EnterAt,
		ExitAt:      i.ExitAt,
		Strategy:    i.Strategy,
		StgVer:      i.StgVer,
		MaxDrawDown: i.MaxDrawDown,
		ProfitRate:  i.ProfitRate,
		Profit:      i.Profit,
		Info:        i.Info,
	})
	if err_ != nil {
		return NewDbErr(core.ErrDbExecFail, err_)
	}
	return nil
}

func (i *IOrder) saveUpdate(sess *Queries) *errs.Error {
	err_ := sess.SetIOrder(context.Background(), SetIOrderParams{
		TaskID:      int32(i.TaskID),
		Symbol:      i.Symbol,
		Sid:         i.Sid,
		Timeframe:   i.Timeframe,
		Short:       i.Short,
		Status:      i.Status,
		EnterTag:    i.EnterTag,
		InitPrice:   i.InitPrice,
		QuoteCost:   i.QuoteCost,
		ExitTag:     i.ExitTag,
		Leverage:    i.Leverage,
		EnterAt:     i.EnterAt,
		ExitAt:      i.ExitAt,
		Strategy:    i.Strategy,
		StgVer:      i.StgVer,
		MaxDrawDown: i.MaxDrawDown,
		ProfitRate:  i.ProfitRate,
		Profit:      i.Profit,
		Info:        i.Info,
		ID:          i.ID,
	})
	if err_ != nil {
		return NewDbErr(core.ErrDbExecFail, err_)
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
		return NewDbErr(core.ErrDbExecFail, err_)
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
		return NewDbErr(core.ErrDbExecFail, err_)
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
	if config.NoDB {
		return nil
	}
	for _, orders := range accOpenODs {
		allOrders := append(HistODs, utils.ValsOfMap(orders)...)
		for _, od := range allOrders {
			od.ID = 0
			err := od.saveToDb(q)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (q *Queries) getIOrders(sql string, args []interface{}) ([]*IOrder, *errs.Error) {
	rows, err_ := q.db.Query(context.Background(), sql, args...)
	if err_ != nil {
		return nil, NewDbErr(core.ErrDbReadFail, err_)
	}
	defer rows.Close()
	var res = make([]*IOrder, 0, 4)
	for rows.Next() {
		var iod IOrder
		err_ = rows.Scan(
			&iod.ID,
			&iod.TaskID,
			&iod.Symbol,
			&iod.Sid,
			&iod.Timeframe,
			&iod.Short,
			&iod.Status,
			&iod.EnterTag,
			&iod.InitPrice,
			&iod.QuoteCost,
			&iod.ExitTag,
			&iod.Leverage,
			&iod.EnterAt,
			&iod.ExitAt,
			&iod.Strategy,
			&iod.StgVer,
			&iod.MaxDrawDown,
			&iod.ProfitRate,
			&iod.Profit,
			&iod.Info,
		)
		if err_ != nil {
			return nil, NewDbErr(core.ErrDbReadFail, err_)
		}
		res = append(res, &iod)
	}
	return res, nil
}

func (q *Queries) getExOrders(sql string, args []interface{}) ([]*ExOrder, *errs.Error) {
	rows, err_ := q.db.Query(context.Background(), sql, args...)
	if err_ != nil {
		return nil, NewDbErr(core.ErrDbReadFail, err_)
	}
	defer rows.Close()
	var res = make([]*ExOrder, 0, 4)
	for rows.Next() {
		var iod ExOrder
		err_ = rows.Scan(
			&iod.ID,
			&iod.TaskID,
			&iod.InoutID,
			&iod.Symbol,
			&iod.Enter,
			&iod.OrderType,
			&iod.OrderID,
			&iod.Side,
			&iod.CreateAt,
			&iod.Price,
			&iod.Average,
			&iod.Amount,
			&iod.Filled,
			&iod.Status,
			&iod.Fee,
			&iod.FeeType,
			&iod.UpdateAt,
		)
		if err_ != nil {
			return nil, NewDbErr(core.ErrDbReadFail, err_)
		}
		res = append(res, &iod)
	}
	return res, nil
}

type GetOrdersArgs struct {
	Strategy    string
	Pairs       []string
	TimeFrame   string
	Status      int   // 0表示所有，1表示未平仓，2表示历史订单
	TaskID      int64 // 0表示所有，>0表示指定任务
	CloseAfter  int64 // 开始时间戳
	CloseBefore int64 // 结束时间戳
	Limit       int
	AfterID     int
	OrderBy     string
}

func (q *Queries) GetOrders(args GetOrdersArgs) ([]*InOutOrder, *errs.Error) {
	var b strings.Builder
	b.WriteString("select ")
	b.WriteString(iOrderFields)
	b.WriteString(" from iorder where 1=1 ")
	sqlParams := make([]interface{}, 0, 2)
	if args.TaskID > 0 {
		b.WriteString(fmt.Sprintf("and task_id=$%v ", len(sqlParams)+1))
		sqlParams = append(sqlParams, args.TaskID)
	}
	if args.AfterID > 0 {
		b.WriteString(fmt.Sprintf(" and id > $%v ", len(sqlParams)+1))
		sqlParams = append(sqlParams, args.AfterID)
	}
	if args.Status >= 1 {
		rel := "<"
		if args.Status == 2 {
			rel = "="
		}
		b.WriteString(fmt.Sprintf("and status%v$%v ", rel, len(sqlParams)+1))
		sqlParams = append(sqlParams, InOutStatusFullExit)
	}
	if args.Strategy != "" {
		b.WriteString(fmt.Sprintf("and strategy=$%v ", len(sqlParams)+1))
		sqlParams = append(sqlParams, args.Strategy)
	}
	if len(args.Pairs) == 1 {
		b.WriteString(fmt.Sprintf("and symbol=$%v ", len(sqlParams)+1))
		sqlParams = append(sqlParams, args.Pairs[0])
	} else if len(args.Pairs) > 1 {
		b.WriteString("and symbol in(")
		for i, pair := range args.Pairs {
			isLast := i == len(args.Pairs)-1
			b.WriteString(fmt.Sprintf("$%v", len(sqlParams)+1))
			if !isLast {
				b.WriteString(",")
			}
			sqlParams = append(sqlParams, pair)
		}
		b.WriteString(") ")
	}
	if args.TimeFrame != "" {
		b.WriteString(fmt.Sprintf("and timeframe=$%v ", len(sqlParams)+1))
		sqlParams = append(sqlParams, args.TimeFrame)
	}
	if args.CloseAfter > 0 {
		b.WriteString(fmt.Sprintf("and exit_at >= $%v ", len(sqlParams)+1))
		sqlParams = append(sqlParams, args.CloseAfter)
	}
	if args.CloseBefore > 0 {
		b.WriteString(fmt.Sprintf("and exit_at < $%v ", len(sqlParams)+1))
		sqlParams = append(sqlParams, args.CloseBefore)
	}
	if args.OrderBy == "" {
		args.OrderBy = "enter_at desc"
	}
	b.WriteString("order by " + args.OrderBy)
	if args.Limit > 0 {
		b.WriteString(fmt.Sprintf(" limit $%v ", len(sqlParams)+1))
		sqlParams = append(sqlParams, args.Limit)
	}
	iorders, err := q.getIOrders(b.String(), sqlParams)
	if err != nil || len(iorders) == 0 {
		return nil, err
	}
	var itemMap = make(map[int64]*InOutOrder)
	b = strings.Builder{}
	b.WriteString("select ")
	b.WriteString(exOrderFields)
	b.WriteString(" from exorder where 1=1 ")
	sqlParams = make([]interface{}, 0, 2)
	if args.TaskID > 0 {
		b.WriteString(fmt.Sprintf("and task_id=$%v ", len(sqlParams)+1))
		sqlParams = append(sqlParams, args.TaskID)
	}
	b.WriteString("and inout_id in (")
	isFirst := true
	for _, od := range iorders {
		itemMap[od.ID] = &InOutOrder{
			IOrder: od,
		}
		if !isFirst {
			b.WriteString(",")
		}
		b.WriteString(fmt.Sprintf("$%v", len(sqlParams)+1))
		sqlParams = append(sqlParams, od.ID)
		isFirst = false
	}
	b.WriteString(")")
	exOrders, err := q.getExOrders(b.String(), sqlParams)
	if err != nil {
		return nil, err
	}
	for _, od := range exOrders {
		iod, ok := itemMap[od.InoutID]
		if !ok {
			continue
		}
		if od.Enter {
			iod.Enter = od
		} else {
			iod.Exit = od
		}
	}
	res := utils.ValsOfMap(itemMap)
	slices.SortFunc(res, func(a, b *InOutOrder) int {
		return int((a.EnterAt - b.EnterAt) / 1000)
	})
	return res, nil
}

func (q *Queries) DelOrder(od *InOutOrder) *errs.Error {
	if od == nil || od.ID == 0 {
		return nil
	}
	openOds, lock := GetOpenODs(GetTaskAcc(od.TaskID))
	lock.Lock()
	delete(openOds, od.ID)
	lock.Unlock()
	// 设置已完成，防止其他地方错误使用
	od.Status = InOutStatusDelete
	ctx := context.Background()
	sql := fmt.Sprintf("delete from iorder where id=%v", od.ID)
	_, err_ := q.db.Exec(ctx, sql)
	if err_ != nil {
		return NewDbErr(core.ErrDbExecFail, err_)
	}
	delExOrder := func(exId int64) *errs.Error {
		sql = fmt.Sprintf("delete from exorder where id=%v", exId)
		_, err_ = q.db.Exec(ctx, sql)
		if err_ != nil {
			return NewDbErr(core.ErrDbExecFail, err_)
		}
		return nil
	}
	if od.Enter != nil && od.Enter.ID > 0 {
		err := delExOrder(od.Enter.ID)
		if err != nil {
			return err
		}
	}
	if od.Exit != nil && od.Exit.ID > 0 {
		return delExOrder(od.Exit.ID)
	}
	return nil
}

/*
GetHistOrderTfs
获取指定任务，指定策略的最新使用的时间周期
*/
func (q *Queries) GetHistOrderTfs(taskId int64, stagy string) (map[string]string, *errs.Error) {
	ctx := context.Background()
	sql := fmt.Sprintf("select DISTINCT ON (symbol) symbol,timeframe from iorder where task_id=%v and strategy=? ORDER BY symbol, enter_at DESC", taskId)
	rows, err_ := q.db.Query(ctx, sql, stagy)
	if err_ != nil {
		return nil, NewDbErr(core.ErrDbReadFail, err_)
	}
	defer rows.Close()
	var result = make(map[string]string)
	for rows.Next() {
		var symbol, timeFrame string
		err_ = rows.Scan(&symbol, &timeFrame)
		if err_ != nil {
			return result, NewDbErr(core.ErrDbReadFail, err_)
		}
		result[symbol] = timeFrame
	}
	return result, nil
}
