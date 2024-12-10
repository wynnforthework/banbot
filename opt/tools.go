package opt

import (
	"encoding/csv"
	"flag"
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	"github.com/xuri/excelize/v2"
	"go.uber.org/zap"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

/*
CompareExgBTOrders
Compare the exchange export order records with the backtest order records.
对比交易所导出订单记录和回测订单记录。
*/
func CompareExgBTOrders(args []string) {
	err := biz.SetupComs(&config.CmdArgs{})
	if err != nil {
		panic(err)
	}
	_, err = orm.LoadMarkets(exg.Default, false)
	if err != nil {
		panic(err)
	}
	var exgPath, btPath, exgName, botName string
	var sub = flag.NewFlagSet("cmp", flag.ExitOnError)
	sub.StringVar(&exgName, "exg", "binance", "exchange name")
	sub.StringVar(&botName, "bot-name", "", "botName for live trade")
	sub.StringVar(&exgPath, "exg-path", "", "exchange order xlsx file")
	sub.StringVar(&btPath, "bt-path", "", "backTest order file")
	err_ := sub.Parse(args)
	if err_ != nil {
		panic(err_)
	}
	if exgPath == "" {
		panic("`exg-path` is required")
	}
	if btPath == "" {
		panic("`bt-path` is required")
	}
	if botName == "" {
		log.Error("bot-name is required")
		return
	}
	btOrders, startMS, endMS := readBackTestOrders(btPath)
	if len(btOrders) == 0 {
		log.Warn("no batcktest orders found")
		return
	}
	f, err_ := excelize.OpenFile(exgPath)
	if err_ != nil {
		panic(err_)
	}
	defer f.Close()
	var exgOrders []*banexg.Order
	switch exgName {
	case "binance":
		exgOrders = readBinanceOrders(f, startMS, endMS, botName)
	default:
		panic("unsupport exchange: " + exgName)
	}
	if len(exgOrders) == 0 {
		log.Warn("no exchange orders to compare")
		return
	}
	pairExgOds := buildExgOrders(exgOrders, botName)
	file, err_ := os.Create(fmt.Sprintf("%s/cmp_orders.csv", filepath.Dir(exgPath)))
	if err_ != nil {
		log.Error("create cmp_orders.csv fail", zap.Error(err_))
		return
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	heads := []string{"tag", "symbol", "timeFrame", "dirt", "entAt", "exitAt", "entPrice", "exitPrice", "Amount",
		"Fee", "Profit", "entDelay", "exitDelay", "priceDiff %", "amtDiff %", "feeDiff %",
		"profitDiff %", "profitDf", "reason"}
	if err_ = writer.Write(heads); err_ != nil {
		log.Error("write orders.csv fail", zap.Error(err_))
		return
	}
	for _, iod := range btOrders {
		tfMSecs := int64(utils2.TFToSecs(iod.Timeframe) * 1000)
		tfMSecsFlt := float64(tfMSecs)
		entFixMS := utils2.AlignTfMSecs(iod.EnterAt, tfMSecs)
		exgOds, _ := pairExgOds[iod.Symbol]
		dirt := "long"
		if iod.Short {
			dirt = "short"
		}
		// Find out if there are matching exchange orders
		// 查找是否有匹配的交易所订单
		var matOd *orm.InOutOrder
		for i, exod := range exgOds {
			if exod.Short == iod.Short && math.Abs(float64(exod.EnterAt-entFixMS)) < tfMSecsFlt {
				matOd = exod
				pairExgOds[iod.Symbol] = append(exgOds[:i], exgOds[i+1:]...)
				break
			}
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
				matOd.Exit = &orm.ExOrder{}
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
	for _, odList := range pairExgOds {
		for _, iod := range odList {
			dirt := "long"
			if iod.Short {
				dirt = "short"
			}
			if iod.Exit == nil {
				iod.Exit = &orm.ExOrder{}
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
}

func readBackTestOrders(path string) ([]*orm.InOutOrder, int64, int64) {
	file, err := os.Open(path)
	if err != nil {
		log.Error("Error opening file:", zap.Error(err))
		return nil, 0, 0
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.Comma = ','
	var pairIdx, tfIdx, dirtIdx, lvgIdx, entIdx, entPriceIdx, entAmtIdx, costIdx, entFeeIdx int
	var exitIdx, exitPriceIdx, exitAmtIdx, exitFeeIdx, profitIdx int
	var res []*orm.InOutOrder
	var startMS, endMS int64
	var maxTfSecs int
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			log.Error("Error reading record:", zap.Error(err))
			return nil, 0, 0
		}
		if costIdx == 0 {
			for idx, title := range row {
				title = strings.TrimSpace(title)
				switch title {
				case "symbol":
					pairIdx = idx
				case "timeframe":
					tfIdx = idx
				case "direction":
					dirtIdx = idx
				case "leverage":
					lvgIdx = idx
				case "entAt":
					entIdx = idx
				case "entPrice":
					entPriceIdx = idx
				case "entAmount":
					entAmtIdx = idx
				case "entCost":
					costIdx = idx
				case "entFee":
					entFeeIdx = idx
				case "exitAt":
					exitIdx = idx
				case "exitPrice":
					exitPriceIdx = idx
				case "exitAmount":
					exitAmtIdx = idx
				case "exitFee":
					exitFeeIdx = idx
				case "profit":
					profitIdx = idx
				}
			}
			if costIdx == 0 {
				log.Error("read orders head fail", zap.Strings("head", row))
				return nil, 0, 0
			}
		} else {
			symbol := row[pairIdx]
			timeFrame := row[tfIdx]
			maxTfSecs = max(maxTfSecs, utils2.TFToSecs(timeFrame))
			isShort := row[dirtIdx] == "short"
			leverage, _ := strconv.ParseFloat(row[lvgIdx], 64)
			enterMS := btime.ParseTimeMS(row[entIdx])
			entPrice, _ := strconv.ParseFloat(row[entPriceIdx], 64)
			entAmount, _ := strconv.ParseFloat(row[entAmtIdx], 64)
			entFee, _ := strconv.ParseFloat(row[entFeeIdx], 64)
			exitMS := btime.ParseTimeMS(row[exitIdx])
			exitPrice, _ := strconv.ParseFloat(row[exitPriceIdx], 64)
			exitAmount, _ := strconv.ParseFloat(row[exitAmtIdx], 64)
			exitFee, _ := strconv.ParseFloat(row[exitFeeIdx], 64)
			profit, _ := strconv.ParseFloat(row[profitIdx], 64)
			if startMS == 0 {
				startMS = enterMS
			} else {
				endMS = max(endMS, exitMS)
			}
			res = append(res, &orm.InOutOrder{
				IOrder: &orm.IOrder{
					Symbol:    symbol,
					Timeframe: timeFrame,
					Short:     isShort,
					Leverage:  leverage,
					EnterAt:   enterMS,
					ExitAt:    exitMS,
					Profit:    profit,
				},
				Enter: &orm.ExOrder{
					Enter:    true,
					Symbol:   symbol,
					CreateAt: enterMS,
					UpdateAt: enterMS,
					Price:    entPrice,
					Average:  entPrice,
					Amount:   entAmount,
					Filled:   entAmount,
					Fee:      entFee,
				},
				Exit: &orm.ExOrder{
					Symbol:   symbol,
					CreateAt: exitMS,
					UpdateAt: exitMS,
					Price:    exitPrice,
					Average:  exitPrice,
					Amount:   exitAmount,
					Filled:   exitAmount,
					Fee:      exitFee,
				},
			})
		}
	}
	// Move the end time back by 2 bars to prevent the exchange order section from being filtered
	// 将结束时间，往后推移2个bar，防止交易所订单部分被过滤
	endMS += int64(maxTfSecs*1000) * 2
	return res, startMS, endMS
}

/*
buildExgOrders
Construct an InOutOrder from an exchange order for comparison; It is not used for real/backtesting
从交易所订单构建InOutOrder用于对比；非实盘/回测时使用
*/
func buildExgOrders(ods []*banexg.Order, clientPrefix string) map[string][]*orm.InOutOrder {
	sort.Slice(ods, func(i, j int) bool {
		return ods[i].LastTradeTimestamp < ods[j].LastTradeTimestamp
	})
	var jobMap = make(map[string][]*orm.InOutOrder)
	for _, od := range ods {
		if od.Filled == 0 {
			continue
		}
		odList, _ := jobMap[od.Symbol]
		newList := make([]*orm.InOutOrder, 0, len(odList))
		if clientPrefix != "" && strings.HasPrefix(od.ClientOrderID, clientPrefix) {
			// 优先通过ClientOrderID匹配
			var openOd *orm.InOutOrder
			for _, iod := range odList {
				if od.ClientOrderID == iod.Enter.OrderID {
					openOd = iod
					break
				}
			}
			if openOd != nil {
				openOd.ExitAt = od.LastUpdateTimestamp
				openOd.Exit = &orm.ExOrder{
					Symbol:   openOd.Symbol,
					CreateAt: od.LastUpdateTimestamp,
					UpdateAt: od.LastUpdateTimestamp,
					Price:    od.Price,
					Average:  od.Average,
					Amount:   openOd.Enter.Amount,
					Filled:   openOd.Enter.Filled,
					Fee:      od.Fee.Cost,
				}
				openOd.Status = orm.InOutStatusFullExit
				openOd.UpdateProfits(od.Average)
				continue
			}
			newList = odList
		} else {
			// An order placed by a non-robot attempts to close the robot's position
			// 非机器人下的订单，尝试平机器人的仓
			for i, iod := range odList {
				if iod.Enter.Side == od.Side || iod.Exit != nil {
					newList = append(newList, iod)
					continue
				}
				part := iod
				curFee := od.Fee.Cost
				if od.Filled < iod.Enter.Amount {
					part = iod.CutPart(od.Filled, od.Filled)
				} else if od.Filled > iod.Enter.Amount {
					rate := iod.Enter.Amount / od.Filled
					curFee = od.Fee.Cost * rate
					od.Fee.Cost -= curFee
				}
				part.ExitAt = od.LastUpdateTimestamp
				part.Exit = &orm.ExOrder{
					Symbol:   part.Symbol,
					CreateAt: od.LastUpdateTimestamp,
					UpdateAt: od.LastUpdateTimestamp,
					Price:    od.Price,
					Average:  od.Average,
					Amount:   part.Enter.Amount,
					Filled:   part.Enter.Filled,
					Fee:      curFee,
				}
				part.Status = orm.InOutStatusFullExit
				part.UpdateProfits(od.Average)
				od.Filled -= iod.Enter.Amount
				newList = append(newList, part)
				if part != iod {
					newList = append(newList, iod)
				}
				if od.Filled <= 0 {
					newList = append(newList, odList[i+1:]...)
					break
				}
			}
		}
		jobMap[od.Symbol] = newList
		if od.Filled <= 0 {
			continue
		}
		iod := &orm.InOutOrder{
			IOrder: &orm.IOrder{
				Symbol:  od.Symbol,
				Short:   od.Side == banexg.OdSideSell,
				EnterAt: od.LastTradeTimestamp,
			},
			Enter: &orm.ExOrder{
				Enter:    true,
				Symbol:   od.Symbol,
				Side:     od.Side,
				CreateAt: od.LastTradeTimestamp,
				UpdateAt: od.LastTradeTimestamp,
				Price:    od.Price,
				Average:  od.Average,
				Amount:   od.Filled,
				Filled:   od.Filled,
				Fee:      od.Fee.Cost,
				OrderID:  od.ClientOrderID,
			},
		}
		jobMap[od.Symbol] = append(newList, iod)
	}
	return jobMap
}

func readBinanceOrders(f *excelize.File, start, stop int64, botName string) []*banexg.Order {
	rowId := 1 // 首个从第2行开始
	sheet := "sheet1"
	colEnd := 'M'
	var res = make([]*banexg.Order, 0, 20)
	var order *banexg.Order
	markets := exg.Default.GetCurMarkets()
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
			alignMS := utils2.AlignTfMSecs(createMS, 60000)
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
		} else if order == nil || strings.Contains(textB, "成交时间") {
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
	return res
}
