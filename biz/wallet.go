package biz

import (
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"math"
	"strings"
)

func (iw *ItemWallet) Total(withUpol bool) float64 {
	sumVal := iw.Available
	for _, v := range iw.Pendings {
		sumVal += v
	}
	for _, v := range iw.Frozens {
		sumVal += v
	}
	if withUpol {
		sumVal += iw.UnrealizedPOL
	}
	return sumVal
}

/*
FiatValue 获取此钱包的法币价值
*/
func (iw *ItemWallet) FiatValue(withUpol bool) float64 {
	return iw.Total(withUpol) * core.GetPrice(iw.Coin)
}

/*
SetMargin
设置保证金占用。优先从unrealized_pol-used_upol中取。不足时从余额中取。

	超出时释放到余额
*/
func (iw *ItemWallet) SetMargin(odKey string, amount float64) *errs.Error {
	// 提取旧保证金占用值
	oldAmt, exists := iw.Frozens[odKey]
	if !exists {
		oldAmt = 0
	}

	avaUpol := iw.UnrealizedPOL - iw.UsedUPol
	upolCost := 0.0

	if avaUpol > 0 {
		// 优先使用可用的未实现盈亏余额
		iw.UsedUPol += amount
		if iw.UsedUPol <= iw.UnrealizedPOL {
			// 未实现盈亏足够，无需冻结
			if _, exists := iw.Frozens[odKey]; exists {
				delete(iw.Frozens, odKey)
			}
			upolCost = amount
			amount = 0
		} else {
			//未实现盈亏不足，更新还需占用的
			newAmount := iw.UsedUPol - iw.UnrealizedPOL
			upolCost = amount - newAmount
			amount = newAmount
			iw.UsedUPol = iw.UnrealizedPOL
		}
	}

	addVal := amount - oldAmt
	if addVal <= 0 {
		//已有保证金超过要求值，释放到余额
		iw.Available -= addVal
	} else {
		//已有保证金不足，从余额中扣除
		if iw.Available < addVal {
			//余额不足
			iw.UsedUPol -= upolCost
			errMsg := "available " + iw.Coin + " Insufficient, frozen require: " + fmt.Sprintf("%.5f", addVal) + ", " + odKey
			return errs.NewMsg(core.ErrLowFunds, errMsg)
		}
		iw.Available -= addVal
	}
	iw.Frozens[odKey] = amount

	return nil
}

/*
SetFrozen
设置冻结金额为固定值。可从余额或pending中同步。

	不足则从另一侧取用，超出则添加到另一侧。
*/
func (iw *ItemWallet) SetFrozen(odKey string, amount float64, withAvailable bool) *errs.Error {
	oldAmt, exists := iw.Frozens[odKey]
	if !exists {
		oldAmt = 0
	}

	addVal := amount - oldAmt
	if withAvailable {
		if addVal > 0 && iw.Available < addVal {
			errMsg := "available " + iw.Coin + " Insufficient, frozen require: " + fmt.Sprintf("%.5f", addVal) + ", " + odKey
			return errs.NewMsg(core.ErrLowFunds, errMsg)
		}
		iw.Available -= addVal
	} else {
		pendVal, exists := iw.Pendings[odKey]
		if !exists {
			pendVal = 0
		}
		if addVal > 0 && pendVal < addVal {
			errMsg := "pending " + iw.Coin + " Insufficient, frozen require: " + fmt.Sprintf("%.5f", addVal) + ", " + odKey
			return errs.NewMsg(core.ErrLowSrcAmount, errMsg)
		}
		iw.Pendings[odKey] = pendVal - addVal
	}
	iw.Frozens[odKey] = amount

	return nil
}

func (iw *ItemWallet) Reset() {
	iw.Available = 0
	iw.UnrealizedPOL = 0
	iw.UsedUPol = 0
	iw.Frozens = make(map[string]float64)
	iw.Pendings = make(map[string]float64)
}

func (w *Wallets) SetWallets(data *config.WalletAmountsConfig) {
	for k, v := range *data {
		item, ok := w.Items[k]
		if !ok {
			item = &ItemWallet{Coin: k}
			w.Items[k] = item
		} else {
			item.Reset()
		}
		item.Available = v
	}
}

func (w *Wallets) Get(code string) *ItemWallet {
	wallet, ok := w.Items[code]
	if !ok {
		wallet = &ItemWallet{Coin: code}
		w.Items[code] = wallet
	}
	return wallet
}

/*
CostAva
从某个币的可用余额中扣除，添加到pending中，仅用于回测
negative : 是否允许负数余额（空单用到）
minRate: 最小开单倍率
return: 实际扣除数量, errs.Error
*/
func (w *Wallets) CostAva(odKey string, symbol string, amount float64, negative bool, minRate float64) (float64, *errs.Error) {
	wallet := w.Get(symbol)
	srcAmount := wallet.Available
	if minRate == 0 {
		minRate = w.MinOpenRate
	}
	var realCost float64
	if srcAmount >= amount || negative {
		//余额充足，或允许负数，直接扣除
		realCost = amount
	} else if srcAmount/amount > minRate {
		//差额在近似允许范围内，扣除实际值
		realCost = srcAmount
	} else {
		return 0, errs.NewMsg(core.ErrLowFunds, "wallet %s balance %.5f < %.5f", symbol, srcAmount, amount)
	}
	log.Debug("CostAva wallet", zap.String("key", odKey), zap.String("coin", symbol),
		zap.Float64("ava", wallet.Available), zap.Float64("cost", realCost))
	wallet.Available -= realCost
	wallet.Pendings[odKey] = realCost
	return realCost, nil
}

/*
CostFrozen

	此方法不用于合约
	从frozen中扣除，如果不够，从available扣除剩余部分
	扣除后，添加到pending中
*/
func (w *Wallets) CostFrozen(odKey string, symbol string, amount float64) float64 {
	wallet, ok := w.Items[symbol]
	if !ok {
		return 0
	}

	frozenAmt, ok := wallet.Frozens[odKey]
	if ok {
		delete(wallet.Frozens, odKey)
	} else {
		frozenAmt = 0
	}

	log.Debug("CostFrozen wallet", zap.String("key", odKey), zap.String("coin", symbol),
		zap.Float64("ava", wallet.Available), zap.Float64("add", frozenAmt-amount))
	// 将冻结的剩余部分归还到available，正负都有可能
	wallet.Available += frozenAmt - amount

	realCost := amount
	if wallet.Available < 0 {
		realCost += wallet.Available
		wallet.Available = 0
	}

	if wallet.Pendings == nil {
		wallet.Pendings = make(map[string]float64)
	}
	wallet.Pendings[odKey] = realCost

	return realCost
}

/*
ConfirmPending
从src中确认扣除，添加到tgt的余额中
*/
func (w *Wallets) ConfirmPending(odKey string, srcKey string, srcAmount float64, tgtKey string, tgtAmount float64, toFrozen bool) bool {
	src, srcExists := w.Items[srcKey]
	if !srcExists {
		return false
	}

	tgt := w.Get(tgtKey)

	pendingAmt, ok := src.Pendings[odKey]
	if !ok {
		return false
	}

	leftPending := pendingAmt - srcAmount
	delete(src.Pendings, odKey)

	log.Debug("ConfirmPending wallet", zap.String("key", odKey), zap.String("from", srcKey),
		zap.Float64("ava", src.Available), zap.Float64("leftPend", leftPending))
	src.Available += leftPending // 剩余的 pending 金额归还到 available（正负都可能）

	if toFrozen {
		tgt.Frozens[odKey] = tgtAmount
	} else {
		tgt.Available += tgtAmount
	}
	return true
}

/*
Cancel
取消对币种的数量锁定(frozens/pendings)，重新加到available上
*/
func (w *Wallets) Cancel(odKey string, symbol string, addAmount float64, fromPending bool) {
	wallet, ok := w.Items[symbol]
	if !ok {
		return
	}

	var srcMap map[string]float64
	if fromPending {
		srcMap = wallet.Pendings
	} else {
		srcMap = wallet.Frozens
	}

	srcAmount, exists := srcMap[odKey]
	if exists {
		delete(srcMap, odKey)
	}
	srcAmount += addAmount

	tag := "frozen"
	if fromPending {
		tag = "pending"
	}
	wallet.Available += srcAmount
	log.Debug("cancel to ava", zap.String("tag", tag), zap.Float64("srcAmt", srcAmount),
		zap.String("od", odKey), zap.String("coin", symbol), zap.Float64("ava", wallet.Available))
}

/*
EnterOd
实盘和模拟都执行，实盘时可防止过度消费

	如果余额不足，会发出异常
	需要调用confirm_od_enter确认。也可调用cancel取消
*/
func (wm *Wallets) EnterOd(od *orm.InOutOrder) (float64, *errs.Error) {
	odKey := od.Key()
	exs := orm.GetSymbolByID(od.Main.Sid)
	if exs == nil {
		panic(fmt.Sprintf("EnterOd invalid sid of order: %v", od.Main.Sid))
	}
	var legalCost float64

	if od.Enter.Amount != 0 {
		legalCost = od.Enter.Amount * core.GetPrice(od.Main.Symbol)
	} else {
		legalCost = od.GetInfoFloat64(orm.OdInfoLegalCost)
	}

	isFuture := exs.Market == "future"
	var quoteCost, quoteMargin float64

	baseCode, quoteCode := exs.BaseQuote()
	if isFuture || !od.Main.Short {
		// 期货合约，现货多单锁定quote
		if legalCost < core.MinStakeAmount {
			return 0, errs.NewMsg(core.ErrInvalidCost, fmt.Sprintf("margin cost must >= %v, cur: %.2f", core.MinStakeAmount, legalCost))
		}

		if isFuture {
			//期货合约，名义价值=保证金*杠杆
			legalCost /= float64(od.Main.Leverage)
		}

		quoteCost = wm.GetAmountByLegal(quoteCode, legalCost)
		quoteCost, err := wm.CostAva(odKey, quoteCode, quoteCost, false, 0)

		if err != nil {
			return 0, err
		}

		// 计算名义数量
		quoteMargin = quoteCost
		if isFuture {
			quoteMargin *= float64(od.Main.Leverage)
		}

		od.Main.QuoteCost, err = exg.PrecCost(nil, od.Main.Symbol, quoteMargin)
		if err != nil {
			return 0, err
		}
	} else {
		// 现货空单，锁定base，允许金额为负
		baseCost := wm.GetAmountByLegal(baseCode, legalCost)
		baseCost, err := wm.CostAva(odKey, baseCode, baseCost, true, 0)
		if err != nil {
			return 0, err
		}
		od.Enter.Amount = baseCost
	}

	return legalCost, nil
}

func (s *Wallets) ConfirmOdEnter(od *orm.InOutOrder, enterPrice float64) {
	if core.ProdMode() {
		return
	}
	exs := orm.GetSymbolByID(od.Main.Sid)
	if exs == nil {
		panic(fmt.Sprintf("EnterOd invalid sid of order: %v", od.Main.Sid))
	}
	subOd := od.Enter
	quoteAmount := enterPrice * subOd.Amount
	curFee := subOd.Fee

	baseCode, quoteCode := exs.BaseQuote()
	if exs.Market == "future" {
		// 期货合约，只锁定定价币，不涉及base币的增加
		quoteAmount /= float64(od.Main.Leverage)
		gotAmt := quoteAmount - curFee
		s.ConfirmPending(od.Key(), quoteCode, quoteAmount, quoteCode, gotAmt, true)
	} else if od.Main.Short {
		// 现货卖，手续费扣U
		gotAmt := quoteAmount - curFee
		s.ConfirmPending(od.Key(), baseCode, subOd.Amount, quoteCode, gotAmt, true)
	} else {
		// 现货买，手续费扣币
		baseAmt := subOd.Amount - curFee
		s.ConfirmPending(od.Key(), quoteCode, quoteAmount, baseCode, baseAmt, false)
	}
}
func (o *Wallets) ExitOd(od *orm.InOutOrder, baseAmount float64) {
	if core.ProdMode() {
		return
	}
	exs := orm.GetSymbolByID(od.Main.Sid)
	if exs == nil {
		panic(fmt.Sprintf("EnterOd invalid sid of order: %v", od.Main.Sid))
	}
	if exs.Market == "future" {
		// 期货合约，不涉及base币的变化。退出订单时，对锁定的定价币平仓释放
		return
	}
	baseCode, quoteCode := exs.BaseQuote()
	if od.Main.Short {
		// 现货空单，从quote的frozen卖，计算到quote的available，从base的pending未成交部分取消
		o.Cancel(od.Key(), baseCode, 0, true)
		// 这里不用预先扣除，价格可能为None
	} else {
		// 现货多单，从base的available卖，计算到quote的available，从quote的pending未成交部分取消
		wallet := o.Get(baseCode)

		if wallet.Available > 0 && wallet.Available < baseAmount || math.Abs(wallet.Available/baseAmount-1) <= 0.01 {
			baseAmount = wallet.Available
			// 取消quote的pending未成交部分
			o.Cancel(od.Key(), quoteCode, 0, true)
		}

		if baseAmount > 0 {
			_, err := o.CostAva(od.Key(), baseCode, baseAmount, false, 0.01)
			if err != nil {
				log.Error("exit order fail", zap.String("od", od.Key()))
			}
		}
	}
}

func (h *Wallets) ConfirmOdExit(od *orm.InOutOrder, exitPrice float64) *errs.Error {
	if core.ProdMode() {
		return nil
	}
	exs := orm.GetSymbolByID(od.Main.Sid)
	if exs == nil {
		panic(fmt.Sprintf("EnterOd invalid sid of order: %v", od.Main.Sid))
	}
	subOd := od.Exit
	curFee := subOd.Fee
	baseCode, quoteCode := exs.BaseQuote()
	odKey := od.Key()
	if exs.Market == "future" {
		//期货合约不涉及base币的变化。退出订单时，对锁定的定价币平仓释放
		//这里profit扣除了入场和出场手续费，前面入场手续费已扣过了，所以这里需要加入场手续费
		h.Cancel(odKey, quoteCode, od.Main.Profit+od.Enter.Fee, false)
	} else if od.Main.Short {
		//空单，优先从quote的frozen买，不兑换为base，再换算为quote的avaiable
		orgAmount := od.Enter.Filled
		if orgAmount > 0 {
			//执行quote买入，中和base的欠债
			h.CostFrozen(odKey, quoteCode, orgAmount*exitPrice)
		}
		if exitPrice < od.Enter.Price {
			//空单，出场价低于入场价，有利润，将冻结的利润置为available
			h.Cancel(odKey, quoteCode, 0, false)
		}
		quoteAmount := exitPrice*orgAmount + curFee
		h.ConfirmPending(odKey, quoteCode, quoteAmount, baseCode, orgAmount, false)
	} else {
		//多单，从base的avaiable卖，兑换为quote的available
		quoteAmount := exitPrice*subOd.Amount - curFee
		h.ConfirmPending(odKey, baseCode, subOd.Amount, quoteCode, quoteAmount, false)
	}

	return nil
}
func (w *Wallets) CutPart(srcKey string, tgtKey string, symbol string, rate float64) {
	item, exists := w.Items[symbol]
	if !exists {
		return
	}

	if value, ok := item.Pendings[srcKey]; ok {
		cutAmt := value * rate
		item.Pendings[tgtKey] += cutAmt
		item.Pendings[srcKey] -= cutAmt
	}

	if value, ok := item.Frozens[srcKey]; ok {
		cutAmt := value * rate
		item.Frozens[tgtKey] += cutAmt
		item.Frozens[srcKey] -= cutAmt
	}
}

/*
UpdateOds
更新订单。目前只针对期货合约订单，需要更新合约订单的保证金比率。

	传入的订单必然都是同一个定价币的订单
	保证金比率： (仓位名义价值 * 维持保证金率 - 维持保证金速算数) / (钱包余额 + 未实现盈亏)
	钱包余额 = 初始净划入余额（含初始保证金） + 已实现盈亏 + 净资金费用 - 手续费
*/
func (w *Wallets) UpdateOds(odList []*orm.InOutOrder) *errs.Error {
	if len(odList) == 0 {
		return nil
	}

	// 所有订单都是同一个定价币，提前获取此币的钱包
	exs := orm.GetSymbolByID(odList[0].Main.Sid)
	if exs == nil {
		panic(fmt.Sprintf("EnterOd invalid sid of order: %v", odList[0].Main.Sid))
	}
	_, quoteCode := exs.BaseQuote()
	wallet := w.Get(quoteCode)

	// 计算是否爆仓
	var totProfit float64
	for _, od := range odList {
		totProfit += od.Main.Profit
	}
	wallet.UnrealizedPOL = totProfit
	wallet.UsedUPol = 0
	if totProfit < 0 {
		marginRatio := math.Abs(totProfit) / wallet.Total(false)
		if marginRatio > 0.99 {
			// 总亏损超过总资产，爆仓
			wallet.Reset()
			return errs.NewMsg(core.ErrLiquidation, "Account Wallet Liquidation")
		}
	}

	exchange, err := exg.GetWith(exs.Exchange, exs.Market, "")
	if err == nil {
		return err
	}
	for _, od := range odList {
		if od.Enter == nil || od.Enter.Filled == 0 {
			continue
		}
		curPrice := core.GetPrice(od.Main.Symbol)
		// 计算名义价值
		quoteValue := od.Enter.Filled * curPrice
		// 计算当前所需保证金
		curMargin := quoteValue / float64(od.Main.Leverage)
		// 判断价格走势和开单方向是否相同
		odDirt := 1.0
		if od.Main.Short {
			odDirt = -1.0
		}
		odKey := od.Key()
		isGood := (curPrice - od.Enter.Average) * odDirt
		if isGood < 0 {
			// 价格走势不同，产生亏损，判断是否自动补充保证金
			if od.Main.Profit > 0 {
				panic(fmt.Sprintf("od profit should < 0: %+v, profit: %f", od, od.Main.Profit))
			}
			// 计算维持保证金
			minMargin := exchange.CalcMaintMargin(od.Main.Symbol, quoteValue) // 要求的最低保证金
			if math.Abs(od.Main.Profit) >= (curMargin-minMargin)*w.MarginAddRate {
				// 当亏损达到初始保证金比例时，为此订单增加保证金避免强平
				lossPct := w.MarginAddRate * 100
				log.Debug("loss addMargin", zap.Float64("lossPct", lossPct),
					zap.String("od", odKey), zap.Float64("profit", od.Main.Profit),
					zap.Float64("margin", curMargin))
				curMargin -= od.Main.Profit
			}
		}
		// 价格走势和预期相同。所需保证金增长
		err := wallet.SetMargin(odKey, curMargin)
		if err != nil {
			log.Debug("cash lack, add margin fail", zap.String("od", odKey), zap.Error(err))
		}
	}
	return nil
}

func (wm *Wallets) GetAmountByLegal(symbol string, legalCost float64) float64 {
	return legalCost / core.GetPrice(symbol)
}

func (w *Wallets) calcLegal(itemAmt func(item *ItemWallet) float64, symbols []string) ([]float64, []string, []float64) {
	var data map[string]*ItemWallet
	if symbols != nil {
		data = make(map[string]*ItemWallet)
		for _, sym := range symbols {
			if item, ok := w.Items[sym]; ok {
				data[sym] = item
			}
		}
	} else {
		data = w.Items
	}

	amounts := make([]float64, 0)
	coins := make([]string, 0)
	prices := make([]float64, 0)

	for key, item := range data {
		var price float64
		if strings.Contains(key, "USD") {
			price = 1
		} else {
			price = core.GetPrice(key)
		}
		amounts = append(amounts, itemAmt(item)*price)
		coins = append(coins, key)
		prices = append(prices, price)
	}

	return amounts, coins, prices
}

func (w *Wallets) calculateTotalLegal(valueExtractor func(*ItemWallet) float64, symbols []string) float64 {
	amounts, _, _ := w.calcLegal(valueExtractor, symbols)
	total := 0.0
	for _, amt := range amounts {
		total += amt
	}
	return total
}

func (w *Wallets) AvaLegal(symbols []string) float64 {
	return w.calculateTotalLegal(func(x *ItemWallet) float64 { return x.Available }, symbols)
}

func (w *Wallets) TotalLegal(symbols []string, withUPol bool) float64 {
	return w.calculateTotalLegal(func(x *ItemWallet) float64 { return x.Total(withUPol) }, symbols)
}

func (w *Wallets) ProfitLegal(symbols []string) float64 {
	return w.calculateTotalLegal(func(x *ItemWallet) float64 { return x.UnrealizedPOL }, symbols)
}

func (w *Wallets) GetWithdrawLegal(symbols []string) float64 {
	return w.calculateTotalLegal(func(x *ItemWallet) float64 { return x.Withdraw }, symbols)
}

/*
WithdrawLegal
从余额提现，从而禁止一部分钱开单。
*/
func (w *Wallets) WithdrawLegal(amount float64, symbols []string) {
	amounts, coins, prices := w.calcLegal(func(x *ItemWallet) float64 { return x.Available }, symbols)
	total := 0.0
	for _, amt := range amounts {
		total += amt
	}

	drawAmts := make([]float64, len(amounts))
	for i, amt := range amounts {
		drawAmts[i] = (amt / total) * amount / prices[i]
	}

	for i, drawAmt := range drawAmts {
		item := w.Items[coins[i]]
		drawAmt = min(drawAmt, item.Available)
		item.Withdraw += drawAmt
		item.Available -= drawAmt
	}
}

/*
FiatValue
返回给定币种的对法币价值。为空时返回所有币种
*/
func (w *Wallets) FiatValue(withUpol bool, symbols ...string) float64 {
	if len(symbols) == 0 {
		for symbol := range w.Items {
			symbols = append(symbols, symbol)
		}
	}

	var totalVal float64
	for _, symbol := range symbols {
		item, exists := w.Items[symbol]
		if !exists {
			continue
		}
		totalVal += item.FiatValue(withUpol)
	}

	return totalVal
}
