package opt

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/banbox/banbot/core"

	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/orm/ormo"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	"github.com/xuri/excelize/v2"
	"go.uber.org/zap"
)

/*
CompareExgBTOrders
Compare the exchange export order records with the backtest order records.
对比交易所导出订单记录和回测订单记录。
*/
func CompareExgBTOrders(args []string) error {
	err := biz.SetupComs(&config.CmdArgs{})
	if err != nil {
		return err
	}
	var exgPath, btPath, exgName, botName string
	var amtRate float64
	var skipUnHitBt bool
	var sub = flag.NewFlagSet("cmp", flag.ExitOnError)
	sub.StringVar(&exgName, "exg", "binance", "exchange name")
	sub.StringVar(&botName, "bot-name", "", "botName for live trade")
	sub.StringVar(&exgPath, "exg-path", "", "exchange order xlsx file")
	sub.StringVar(&btPath, "bt-path", "", "backTest order file")
	sub.Float64Var(&amtRate, "amt-rate", 0.1, "amt rate threshold, 0~1")
	sub.BoolVar(&skipUnHitBt, "skip-unhit", true, "skip unhit pairs for backtest order")
	err_ := sub.Parse(args)
	if err_ != nil {
		return err_
	}
	if exgPath == "" || btPath == "" || botName == "" {
		return errors.New("`exg-path` `bt-path` bot-name is required")
	}
	btOrders, startMS, endMS, err_ := readBackTestOrders(btPath)
	if err_ != nil {
		return err_
	}
	if len(btOrders) == 0 {
		return errors.New("no batcktest orders found")
	}
	exs := orm.GetSymbolByID(int32(btOrders[0].Sid))
	exchange, err := exg.GetWith(exs.Exchange, exs.Market, "")
	if err != nil {
		return err
	}
	_, err = orm.LoadMarkets(exchange, false)
	if err != nil {
		return err
	}
	f, err_ := excelize.OpenFile(exgPath)
	if err_ != nil {
		return err_
	}
	defer f.Close()
	var exgOrders []*banexg.Order
	switch exgName {
	case "binance":
		exgOrders, err = readBinanceOrders(f, exchange, startMS, endMS, botName)
	default:
		return errors.New("unsupport exchange: " + exgName)
	}
	if err != nil {
		return err
	}
	if len(exgOrders) == 0 {
		return errors.New("no exchange orders to compare")
	}
	pairExgOds := buildExgOrders(exgOrders, botName)
	exgOdList := make([]*ormo.InOutOrder, 0)
	for _, odList := range pairExgOds {
		exgOdList = append(exgOdList, odList...)
	}
	outPath := fmt.Sprintf("%s/cmp_orders.csv", filepath.Dir(exgPath))
	file, err_ := os.Create(outPath)
	if err_ != nil {
		return err_
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	heads := []string{"tag", "symbol", "timeFrame", "dirt", "entAt", "exitAt", "entPrice", "exitPrice", "Amount",
		"Fee", "Profit", "entDelay", "exitDelay", "priceDiff %", "amtDiff %", "feeDiff %",
		"profitDiff %", "profitDf", "reason"}
	if err_ = writer.Write(heads); err_ != nil {
		return err_
	}
	for _, iod := range btOrders {
		tfMSecs := int64(utils2.TFToSecs(iod.Timeframe) * 1000)
		tfMSecsFlt := float64(tfMSecs)
		entFixMS := utils2.AlignTfMSecs(iod.EnterAt, tfMSecs)
		exgOds, _ := pairExgOds[iod.Symbol]
		if skipUnHitBt && len(exgOds) == 0 {
			continue
		}
		dirt := "long"
		if iod.Short {
			dirt = "short"
		}
		// Find out if there are matching exchange orders
		// 查找是否有匹配的交易所订单
		var matches []*ormo.InOutOrder
		for _, exod := range exgOds {
			if exod.Short == iod.Short && math.Abs(float64(exod.EnterAt-entFixMS)) < tfMSecsFlt {
				amtRate2 := exod.Enter.Filled / iod.Enter.Filled
				if math.Abs(amtRate2-1) <= amtRate {
					matches = append(matches, exod)
				}
			}
		}
		var matOd *ormo.InOutOrder
		if len(matches) > 1 {
			slices.SortFunc(matches, func(a, b *ormo.InOutOrder) int {
				diffA := math.Abs(float64(a.ExitAt-iod.ExitAt) / 1000)
				diffB := math.Abs(float64(b.ExitAt-iod.ExitAt) / 1000)
				return int(diffA - diffB)
			})
		}
		if len(matches) > 0 {
			matOd = matches[0]
			unMatches := make([]*ormo.InOutOrder, 0, len(exgOds))
			for _, exod := range exgOds {
				if exod == matOd {
					continue
				}
				unMatches = append(unMatches, exod)
			}
			pairExgOds[iod.Symbol] = unMatches
		}
		if matOd == nil {
			// There is no corresponding record for backtesting orders
			// 回测订单没有对应记录
			entMSStr := btime.ToDateStr(iod.EnterAt, "")
			exitMSStr := btime.ToDateStr(iod.ExitAt, "")
			entPriceStr := strconv.FormatFloat(iod.Enter.Price, 'f', 6, 64)
			amtStr := strconv.FormatFloat(iod.Enter.Filled+iod.Exit.Filled, 'f', 6, 64)
			feeStr := strconv.FormatFloat(iod.Enter.Fee+iod.Exit.Fee, 'f', 6, 64)
			exitPriceStr := strconv.FormatFloat(iod.Exit.Price, 'f', 6, 64)
			profitStr := strconv.FormatFloat(iod.Profit, 'f', 6, 64)
			err_ = writer.Write([]string{"bt", iod.Symbol, iod.Timeframe, dirt, entMSStr, exitMSStr, entPriceStr,
				exitPriceStr, amtStr, feeStr, profitStr, "0", "0", "", "", "", "", "", ""})
			if err_ != nil {
				log.Error("writer csv fail", zap.Error(err_))
			}
		} else {
			// 有匹配记录
			if matOd.Exit == nil {
				matOd.Exit = &ormo.ExOrder{}
			}
			entMSStr := btime.ToDateStr(matOd.EnterAt, "")
			exitMSStr := btime.ToDateStr(matOd.ExitAt, "")
			entPriceStr := strconv.FormatFloat(matOd.Enter.Average, 'f', 6, 64)
			amtStr := strconv.FormatFloat(matOd.Enter.Filled+matOd.Exit.Filled, 'f', 6, 64)
			feeStr := strconv.FormatFloat(matOd.Enter.Fee+matOd.Exit.Fee, 'f', 6, 64)
			profitStr := strconv.FormatFloat(matOd.Profit, 'f', 6, 64)
			exitPriceStr := strconv.FormatFloat(matOd.Exit.Average, 'f', 6, 64)
			entDelay := matOd.EnterAt - iod.EnterAt
			exitDelay := matOd.ExitAt - iod.ExitAt
			entDelayStr := strconv.FormatInt(entDelay/1000, 10)
			exitDelayStr := strconv.FormatInt(exitDelay/1000, 10)
			priceDf := (matOd.Enter.Average - iod.Enter.Average) - (matOd.Exit.Average - iod.Exit.Average)
			priceDiff := strconv.FormatFloat(priceDf*100/iod.Enter.Average, 'f', 1, 64)
			amtDf := (matOd.Enter.Filled - iod.Enter.Filled) - (matOd.Exit.Filled - iod.Exit.Filled)
			amountDiff := strconv.FormatFloat(amtDf*100/iod.Enter.Filled, 'f', 1, 64)
			feeDf := (matOd.Enter.Fee - iod.Enter.Fee) + (matOd.Exit.Fee - iod.Exit.Fee)
			feeDiff := strconv.FormatFloat(feeDf*50/iod.Enter.Fee, 'f', 1, 64)
			profitDf := matOd.Profit - iod.Profit
			profitDfPct := profitDf * 100 / iod.Profit
			profitDiff := strconv.FormatFloat(profitDfPct, 'f', 1, 64)
			profitDfStr := strconv.FormatFloat(profitDf, 'f', 6, 64)
			reason := "OK"
			if math.Abs(float64(entDelay)) < tfMSecsFlt && math.Abs(float64(exitDelay)) < tfMSecsFlt {
				// The time of entry and exit is matched
				// 入场和出场的时间匹配
				if math.Abs(profitDfPct) < 20 {
					reason = "OK"
				} else {
					reason = "Slop"
				}
			} else {
				reason = "Wrong"
			}
			err_ = writer.Write([]string{"same", iod.Symbol, iod.Timeframe, dirt, entMSStr, exitMSStr, entPriceStr,
				exitPriceStr, amtStr, feeStr, profitStr, entDelayStr, exitDelayStr, priceDiff, amountDiff,
				feeDiff, profitDiff, profitDfStr, reason})
			if err_ != nil {
				log.Error("writer csv fail", zap.Error(err_))
			}
		}
	}
	// append unmatch exchange orders
	for _, odList := range pairExgOds {
		for _, iod := range odList {
			dirt := "long"
			if iod.Short {
				dirt = "short"
			}
			if iod.Exit == nil {
				iod.Exit = &ormo.ExOrder{}
			}
			entMSStr := btime.ToDateStr(iod.EnterAt, "")
			exitMSStr := btime.ToDateStr(iod.ExitAt, "")
			entPriceStr := strconv.FormatFloat(iod.Enter.Average, 'f', 6, 64)
			amtStr := strconv.FormatFloat(iod.Enter.Filled+iod.Exit.Filled, 'f', 6, 64)
			feeStr := strconv.FormatFloat(iod.Enter.Fee+iod.Exit.Fee, 'f', 6, 64)
			profitStr := strconv.FormatFloat(iod.Profit, 'f', 6, 64)
			exitPriceStr := strconv.FormatFloat(iod.Exit.Average, 'f', 6, 64)
			err_ = writer.Write([]string{"exg", iod.Symbol, iod.Timeframe, dirt, entMSStr, exitMSStr, entPriceStr,
				exitPriceStr, amtStr, feeStr, profitStr, "0", "0", "", "", "", "", "", ""})
			if err_ != nil {
				log.Error("writer csv fail", zap.Error(err_))
			}
		}
	}
	log.Info("dump compare result", zap.String("at", outPath))
	outPath = fmt.Sprintf("%s/exg_orders.csv", filepath.Dir(exgPath))
	log.Info("dump exchange orders", zap.String("at", outPath))
	return DumpOrdersCSV(exgOdList, outPath)
}

func readBackTestOrders(path string) ([]*ormo.InOutOrder, int64, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, 0, err
	}
	if info.IsDir() {
		path = filepath.Join(path, "orders.gob")
	} else if !strings.HasSuffix(path, ".gob") {
		return nil, 0, 0, errors.New("orders.gob path is required")
	}
	orders, err2 := ormo.LoadOrdersGob(path)
	if err2 != nil {
		return nil, 0, 0, err2
	}
	var startMS, endMS int64
	var maxTfSecs int
	for _, od := range orders {
		tfSecs := utils2.TFToSecs(od.Timeframe)
		if tfSecs > maxTfSecs {
			maxTfSecs = tfSecs
		}
		if startMS == 0 || od.Enter.CreateAt < startMS {
			startMS = od.Enter.CreateAt
		}
		if od.Exit != nil && od.Exit.UpdateAt > endMS {
			endMS = od.Exit.UpdateAt
		}
	}
	// Move the end time back by 2 bars to prevent the exchange order section from being filtered
	// 将结束时间，往后推移2个bar，防止交易所订单部分被过滤
	endMS += int64(maxTfSecs*1000) * 2
	return orders, startMS, endMS, nil
}

/*
handleExitOrders 处理平仓订单，返回更新后的订单列表和已使用的平仓数量
remainFilled: 剩余需要平仓的数量
od: 交易所订单
odList: 当前持仓订单列表
excludeOd: 需要排除的订单（已经通过ClientOrderID匹配的订单）
返回值: 更新后的订单列表, 已使用的平仓数量
*/
func handleExitOrders(remainFilled float64, od *banexg.Order, odList []*ormo.InOutOrder, excludeOd *ormo.InOutOrder) ([]*ormo.InOutOrder, float64) {
	newList := make([]*ormo.InOutOrder, 0, len(odList))
	usedAmount := float64(0)
	initAmt := remainFilled
	for _, iod := range odList {
		if iod == excludeOd || iod.Exit != nil || iod.Enter.Side == od.Side {
			newList = append(newList, iod)
			continue
		}
		part := iod
		curFee := od.Fee.Cost
		exitAmount := iod.Enter.Amount
		if remainFilled < iod.Enter.Amount*0.99 {
			exitAmount = remainFilled
			part = iod.CutPart(remainFilled, remainFilled)
			rate := remainFilled / od.Filled
			curFee = od.Fee.Cost * rate
		} else {
			rate := iod.Enter.Amount / od.Filled
			curFee = od.Fee.Cost * rate
		}
		part.ExitAt = od.LastUpdateTimestamp
		part.Exit = &ormo.ExOrder{
			Symbol:   part.Symbol,
			CreateAt: od.LastUpdateTimestamp,
			UpdateAt: od.LastUpdateTimestamp,
			Price:    od.Price,
			Average:  od.Average,
			Amount:   part.Enter.Amount,
			Filled:   part.Enter.Filled,
			Fee:      curFee,
		}
		part.Status = ormo.InOutStatusFullExit
		part.UpdateProfits(od.Average)
		remainFilled -= exitAmount
		usedAmount += exitAmount
		newList = append(newList, part)
		if part != iod {
			newList = append(newList, iod)
		}
		if remainFilled <= initAmt*0.01 {
			break
		}
	}
	return newList, usedAmount
}

/*
buildExgOrders
Construct an InOutOrder from an exchange order for comparison; It is not used for real/backtesting
从交易所订单构建InOutOrder用于对比；非实盘/回测时使用
*/
func buildExgOrders(ods []*banexg.Order, clientPrefix string) map[string][]*ormo.InOutOrder {
	// 按ClientOrderID分组订单
	orderMap := make(map[string][]*banexg.Order)
	for _, od := range ods {
		if od.Filled == 0 {
			continue
		}
		orderMap[od.ClientOrderID] = append(orderMap[od.ClientOrderID], od)
	}
	jobMap := make(map[string][]*ormo.InOutOrder)
	var temps []*banexg.Order

	for _, orders := range orderMap {
		if len(orders) != 2 {
			// 不是配对订单，加入temps
			temps = append(temps, orders...)
			continue
		}

		// 确保orders[0]是较早的订单
		if orders[0].LastTradeTimestamp > orders[1].LastTradeTimestamp {
			orders[0], orders[1] = orders[1], orders[0]
		}

		// 检查是否是一买一卖
		if orders[0].Side == orders[1].Side {
			temps = append(temps, orders...)
			continue
		}
		ent, exit := orders[0], orders[1]
		if !strings.HasPrefix(ent.ClientOrderID, clientPrefix) || !strings.HasPrefix(exit.ClientOrderID, clientPrefix) {
			temps = append(temps, orders...)
			continue
		}

		// 创建InOutOrder
		iod := &ormo.InOutOrder{
			IOrder: &ormo.IOrder{
				Symbol:  ent.Symbol,
				Short:   ent.Side == banexg.OdSideSell,
				EnterAt: ent.LastTradeTimestamp,
				ExitAt:  exit.LastTradeTimestamp,
				Status:  ormo.InOutStatusFullExit,
			},
			Enter: &ormo.ExOrder{
				Enter:    true,
				Symbol:   ent.Symbol,
				Side:     ent.Side,
				CreateAt: ent.LastTradeTimestamp,
				UpdateAt: ent.LastTradeTimestamp,
				Price:    ent.Price,
				Average:  ent.Average,
				Amount:   ent.Filled,
				Filled:   ent.Filled,
				Fee:      ent.Fee.Cost,
				OrderID:  ent.ClientOrderID,
			},
			Exit: &ormo.ExOrder{
				Symbol:   exit.Symbol,
				CreateAt: exit.LastTradeTimestamp,
				UpdateAt: exit.LastTradeTimestamp,
				Price:    exit.Price,
				Average:  exit.Average,
				Amount:   exit.Filled, // 使用入场订单的数量
				Filled:   exit.Filled,
				Fee:      exit.Fee.Cost,
			},
		}

		// 将InOutOrder添加到结果map
		jobMap[ent.Symbol] = append(jobMap[ent.Symbol], iod)

		inoutRate := exit.Filled / ent.Filled
		if inoutRate > 1.01 {
			// 平仓数量未耗尽，同时平仓其他订单
			remainOrder := *exit
			remainOrder.Filled = exit.Filled - ent.Filled
			remainOrder.Fee.Cost = exit.Fee.Cost * (remainOrder.Filled / exit.Filled)
			temps = append(temps, &remainOrder)
			iod.Exit.Amount = ent.Filled
			iod.Exit.Filled = ent.Filled
			iod.Exit.Fee = exit.Fee.Cost * (ent.Fee.Cost / exit.Fee.Cost)
		} else if inoutRate < 0.99 {
			// 未完全平仓
			remainOrder := *ent
			remainOrder.Filled = ent.Filled - exit.Filled
			remainOrder.Fee.Cost = ent.Fee.Cost * (remainOrder.Filled / ent.Filled)
			temps = append(temps, &remainOrder)
			iod.Enter.Amount = exit.Filled
			iod.Enter.Filled = exit.Filled
			iod.Enter.Fee = ent.Fee.Cost * (exit.Fee.Cost / ent.Fee.Cost)
		}
		iod.UpdateProfits(exit.Average)
	}

	// 处理未配对订单
	if len(temps) > 0 {
		// 按时间排序temps
		sort.Slice(temps, func(i, j int) bool {
			return temps[i].LastTradeTimestamp < temps[j].LastTradeTimestamp
		})

		// 对每个未配对订单执行原来的逻辑
		for _, od := range temps {
			odList, _ := jobMap[od.Symbol]
			newList := make([]*ormo.InOutOrder, 0, len(odList))
			remainFilled := od.Filled

			// 尝试平仓现有持仓
			var usedAmount float64
			newList, usedAmount = handleExitOrders(remainFilled, od, odList, nil)
			remainFilled -= usedAmount

			jobMap[od.Symbol] = newList
			if remainFilled <= od.Filled*0.01 || !strings.HasPrefix(od.ClientOrderID, clientPrefix) {
				continue
			}

			// 创建新的入场订单
			iod := &ormo.InOutOrder{
				IOrder: &ormo.IOrder{
					Symbol:  od.Symbol,
					Short:   od.Side == banexg.OdSideSell,
					EnterAt: od.LastTradeTimestamp,
					Status:  ormo.InOutStatusFullEnter,
				},
				Enter: &ormo.ExOrder{
					Enter:    true,
					Symbol:   od.Symbol,
					Side:     od.Side,
					CreateAt: od.LastTradeTimestamp,
					UpdateAt: od.LastTradeTimestamp,
					Price:    od.Price,
					Average:  od.Average,
					Amount:   remainFilled,
					Filled:   remainFilled,
					Fee:      od.Fee.Cost * (remainFilled / od.Filled),
					OrderID:  od.ClientOrderID,
				},
			}
			jobMap[od.Symbol] = append(newList, iod)
		}
	}

	return jobMap
}

func readBinanceOrders(f *excelize.File, exchange banexg.BanExchange, start, stop int64, botName string) ([]*banexg.Order, *errs.Error) {
	// TODO go-i18n
	rowId := 1 // 首个从第2行开始
	sheet := "sheet1"
	colEnd := 'M'
	var res = make([]*banexg.Order, 0, 20)
	var order *banexg.Order
	markets := exchange.GetCurMarkets()
	if len(markets) == 0 {
		exInfo := exchange.Info()
		return res, errs.NewMsg(errs.CodeParamInvalid, "no markets for %v.%v", exInfo.ID, exInfo.MarketType)
	}
	idMap := make(map[string]string)
	for symbol, mar := range markets {
		idMap[mar.ID] = symbol
	}
	reNonAl := regexp.MustCompile("[a-zA-Z\u4e00-\u9fa5]+")
	for {
		rowId += 1
		rowTxt := strconv.Itoa(rowId)
		// 读取当前行到字典
		row := make(map[string]string)
		for char := 'A'; char <= colEnd; char++ {
			col := string(char)
			text, err_ := f.GetCellValue(sheet, col+rowTxt)
			if err_ != nil {
				log.Error("read cell fail", zap.String("loc", col+rowTxt), zap.Error(err_))
				continue
			}
			row[col] = text
		}
		textA, _ := row["A"]
		textB, _ := row["B"]
		if textA == "" && textB == "" {
			break
		}
		if strings.HasPrefix(textA, "20") {
			// 开始新的订单
			if order != nil {
				res = append(res, order)
				order = nil
			}
			stateStr, _ := row["K"]
			if stateStr == "已撤销" || stateStr == "已过期" {
				continue
			}
			createMS := btime.ParseTimeMS(textA)
			alignMS := int64(math.Round(float64(createMS)/60000)) * 60000
			clientID, _ := row["C"]
			if alignMS < start || alignMS >= stop && strings.HasPrefix(clientID, botName) {
				// 允许截止时间之后的非机器人订单，用于平仓
				continue
			}
			marketID, _ := row["D"]
			symbol, _ := idMap[marketID]
			side, _ := row["E"]
			if side == "卖出" {
				side = banexg.OdSideSell
			} else {
				side = banexg.OdSideBuy
			}
			priceStr, _ := row["F"]
			price, _ := strconv.ParseFloat(priceStr, 64)
			amountStr, _ := row["G"]
			amount, _ := strconv.ParseFloat(amountStr, 64)
			averageStr, _ := row["H"]
			average, _ := strconv.ParseFloat(averageStr, 64)
			filledStr, _ := row["I"]
			filled, _ := strconv.ParseFloat(filledStr, 64)
			costStr, _ := row["J"]
			cost, _ := strconv.ParseFloat(costStr, 64)
			oidParts := strings.Split(clientID, "_")
			if len(oidParts) == 3 {
				clientID = strings.Join(oidParts[:2], "_")
			}
			order = &banexg.Order{
				Timestamp:     createMS,
				ID:            textB,
				ClientOrderID: clientID,
				Symbol:        symbol,
				Side:          side,
				Price:         price,
				Amount:        amount,
				Average:       average,
				Filled:        filled,
				Cost:          cost,
				Fee:           &banexg.Fee{},
			}
			if stateStr == "已成交" {
				order.Status = banexg.OdStatusFilled
			} else if stateStr == "开放" {
				order.Status = banexg.OdStatusOpen
			} else {
				log.Error("unknown order state: " + stateStr)
			}
		} else if order == nil || strings.Contains(textB, "(UTC)") {
			continue
		} else if textA == "" && textB != "" {
			// 成交记录
			curMS := btime.ParseTimeMS(textB)
			feeStr, _ := row["F"]
			feeStr = reNonAl.ReplaceAllString(feeStr, "")
			feeVal, _ := strconv.ParseFloat(feeStr, 64)
			order.Fee.Cost += feeVal
			order.LastTradeTimestamp = curMS
			order.LastUpdateTimestamp = curMS
		}
	}
	if order != nil {
		res = append(res, order)
	}
	return res, nil
}

type AssetData struct {
	Title    string     `json:"title"`
	Labels   []string   `json:"labels"`
	Datasets []*ChartDs `json:"datasets"`
	Times    []int64    // 存储解析后的时间戳
}

/*
MergeAssetsHtml 合并多个assets.html文件的曲线到一个html文件中

files assets.html文件路径Map，键是路径，值是代表此文件的字符串ID
outPath 输出文件路径
lines 需要提取的曲线名称列表，默认为["Real", "Available"]
*/
func MergeAssetsHtml(outPath string, files map[string]string, tags []string, useRate bool) *errs.Error {
	if len(files) <= 1 {
		return errs.NewMsg(errs.CodeParamRequired, "at least 2 files need to merge")
	}
	if len(tags) == 0 {
		tags = []string{"Real", "Available"}
	}
	linesMap := make(map[string]bool)
	for _, line := range tags {
		linesMap[line] = true
	}

	// 读取所有文件的数据
	var allData []*AssetData
	var minTime, maxTime int64 = math.MaxInt64, 0
	for file, prefix := range files {
		data, err := readAssetHtml(file, prefix, useRate)
		if err != nil {
			return err
		}
		allData = append(allData, data)
		// 更新整体时间范围
		if len(data.Times) > 0 {
			minTime = min(minTime, data.Times[0])
			maxTime = max(maxTime, data.Times[len(data.Times)-1])
		}
	}

	// 确定采样数量和间隔
	maxSamples := 0
	for _, data := range allData {
		maxSamples = max(maxSamples, len(data.Labels))
	}

	// 生成最终的时间戳和标签
	interval := (maxTime - minTime) / int64(maxSamples-1)
	dateLay := core.DefaultDateFmt
	if interval >= utils2.SecsDay*1000 {
		dateLay = core.DateFmt
	}
	var labels = make([]string, 0, maxSamples+3)
	var finalTimes = make([]int64, 0, maxSamples+3)
	for i := 0; i < maxSamples; i++ {
		t := minTime + interval*int64(i)
		finalTimes = append(finalTimes, t)
		labels = append(labels, btime.ToDateStr(t, dateLay))
	}
	finalTimes = append(finalTimes, maxTime)
	labels = append(labels, btime.ToDateStr(maxTime, dateLay))

	// 合并数据集
	var datasets []*ChartDs
	for _, data := range allData {
		for _, ds := range data.Datasets {
			if _, ok := linesMap[ds.Label]; !ok {
				continue
			}
			values := make([]float64, 0, len(finalTimes))
			curVal := math.NaN()
			idx := 0
			nextTime, nextVal := data.Times[0], ds.Data[0]
			for _, curTime := range finalTimes {
				for curTime >= nextTime {
					curVal = nextVal
					idx += 1
					if idx < len(data.Times) {
						nextTime, nextVal = data.Times[idx], ds.Data[idx]
					} else {
						nextTime = math.MaxInt64
					}
				}
				values = append(values, curVal)
			}

			// 添加到最终数据集
			label := fmt.Sprintf("%s_%s", data.Title, ds.Label)
			datasets = append(datasets, &ChartDs{
				Label: label,
				Data:  values,
			})
		}
	}

	// 生成最终的图表
	title := "Merged Assets Comparison"
	return DumpChart(outPath, title, labels, 5, nil, datasets)
}

// readAssetHtml 读取assets.html文件并解析数据
func readAssetHtml(file, prefix string, useRate bool) (*AssetData, *errs.Error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, errs.New(errs.CodeIOReadFail, err)
	}

	// 提取JSON数据
	start := strings.Index(string(content), "var chartData = ") + 15
	end := strings.Index(string(content)[start:], "\n")
	if start < 15 || end < 0 {
		return nil, errs.New(errs.CodeInvalidData, fmt.Errorf("invalid html format in file %s", file))
	}
	jsonStr := string(content)[start : start+end]

	var data AssetData
	if err = utils2.UnmarshalString(jsonStr, &data, utils2.JsonNumDefault); err != nil {
		return nil, errs.New(errs.CodeUnmarshalFail, err)
	}

	// 解析时间标签为时间戳
	data.Times = make([]int64, len(data.Labels))
	for i, label := range data.Labels {
		data.Times[i] = btime.ParseTimeMS(label)
	}
	if prefix == "" {
		prefix = filepath.Base(filepath.Dir(file))
	}
	data.Title = prefix
	if useRate {
		for _, ds := range data.Datasets {
			initVal := ds.Data[0]
			if initVal != 0 {
				resArr := make([]float64, 0, len(ds.Data))
				for _, curVal := range ds.Data {
					resArr = append(resArr, curVal/initVal)
				}
			}
		}
	}

	return &data, nil
}
