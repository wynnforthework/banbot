package opt

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	"github.com/bytedance/sonic"
	"github.com/olekukonko/tablewriter"
	"go.uber.org/zap"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type BTResult struct {
	MaxOpenOrders   int        `json:"maxOpenOrders"`
	MinReal         float64    `json:"minReal"`
	MaxReal         float64    `json:"maxReal"`         // Maximum Assets 最大资产
	MaxDrawDownPct  float64    `json:"maxDrawDownPct"`  // Maximum drawdown percentage 最大回撤百分比
	ShowDrawDownPct float64    `json:"showDrawDownPct"` // Displays the maximum drawdown percentage 显示最大回撤百分比
	BarNum          int        `json:"barNum"`
	TimeNum         int        `json:"timeNum"`
	OrderNum        int        `json:"orderNum"`
	lastTime        int64      // 上次bar的时间戳
	Plots           *PlotData  `json:"plots"`
	StartMS         int64      `json:"startMS"`
	EndMS           int64      `json:"endMS"`
	PlotEvery       int        `json:"plotEvery"`
	TotalInvest     float64    `json:"totalInvest"`
	OutDir          string     `json:"outDir"`
	PairGrps        []*RowItem `json:"pairGrps"`
	DateGrps        []*RowItem `json:"dateGrps"`
	EnterGrps       []*RowItem `json:"enterGrps"`
	ExitGrps        []*RowItem `json:"exitGrps"`
	ProfitGrps      []*RowItem `json:"profitGrps"`
	TotProfit       float64    `json:"totProfit"`
	TotCost         float64    `json:"totCost"`
	TotFee          float64    `json:"totFee"`
	TotProfitPct    float64    `json:"totProfitPct"`
	SharpeRatio     float64    `json:"sharpeRatio"`
	SortinoRatio    float64    `json:"sortinoRatio"`
}

type PlotData struct {
	Labels        []string  `json:"labels"`
	OdNum         []int     `json:"odNum"`
	Real          []float64 `json:"real"`
	Available     []float64 `json:"available"`
	UnrealizedPOL []float64 `json:"unrealizedPOL"`
	WithDraw      []float64 `json:"withDraw"`
	tmpOdNum      int
}

type RowPart struct {
	WinCount     int               `json:"winCount"`
	ProfitSum    float64           `json:"profitSum"`
	ProfitPctSum float64           `json:"profitPctSum"`
	CostSum      float64           `json:"costSum"`
	Durations    []int             `json:"durations"`
	Orders       []*orm.InOutOrder `json:"-"`
	Sharpe       float64           `json:"sharpe"` // 夏普比率
	Sortino      float64           `json:"sortino"`
}

type RowItem struct {
	Title string `json:"title"`
	RowPart
}

var (
	PairPickers = make(map[string]func(r *BTResult) []string)
)

func NewBTResult() *BTResult {
	res := &BTResult{
		Plots:     &PlotData{},
		PlotEvery: 1,
	}
	return res
}

func (r *BTResult) printBtResult() {
	if config.StrtgPerf != nil && config.StrtgPerf.Enable {
		core.DumpPerfs(r.OutDir)
	}

	orders := orm.HistODs
	var b strings.Builder
	var tblText string
	if len(orders) > 0 {
		items := []struct {
			Title  string
			Handle func(*BTResult) string
		}{
			{Title: " Pair Profits ", Handle: textGroupPairs},
			{Title: " Date Profits ", Handle: textGroupDays},
			{Title: " Profit Ranges ", Handle: textGroupProfitRanges},
			{Title: " Enter Tag ", Handle: textGroupEntTags},
			{Title: " Exit Tag ", Handle: textGroupExitTags},
		}
		r.groupByPairs(orders)
		r.groupByDates(orders)
		r.groupByProfits(orders)
		r.groupByEnters(orders)
		r.groupByExits(orders)
		for _, item := range items {
			tblText = item.Handle(r)
			if tblText != "" {
				width := strings.Index(tblText, "\n")
				head := utils.PadCenter(item.Title, width, "=")
				b.WriteString(head)
				b.WriteString("\n")
				b.WriteString(tblText)
				b.WriteString("\n")
			}
		}
	} else {
		b.WriteString("No Orders Found\n")
	}
	r.Collect()
	b.WriteString(r.textMetrics(orders))
	log.Info("BackTest Reports:\n" + b.String())

	r.dumpBtFiles()
}

func (r *BTResult) dumpBtFiles() {
	r.dumpOrders(orm.HistODs)

	r.dumpConfig()

	r.dumpStrategy()

	r.dumpStratOutputs()

	r.dumpGraph()

	r.dumpDetail("")
}

func (r *BTResult) Collect() {
	orders := orm.HistODs
	r.OrderNum = len(orders)
	sumProfit := float64(0)
	sumFee := float64(0)
	sumCost := float64(0)
	for _, od := range orders {
		sumProfit += od.Profit
		sumFee += od.Enter.Fee
		if od.Exit != nil {
			sumFee += od.Exit.Fee
		}
		sumCost += od.EnterCost() / od.Leverage
	}
	r.TotProfit = sumProfit
	r.TotCost = sumCost
	r.TotFee = sumFee
	r.TotProfitPct = r.TotProfit * 100 / r.TotalInvest
	if r.MinReal > r.MaxReal {
		r.MinReal = r.MaxReal
	}
	// Calculate the maximum drawdown on the chart
	// 计算图表上的最大回撤
	r.ShowDrawDownPct = r.Plots.calcDrawDown() * 100
	r.groupByPairs(orders)
	r.groupByDates(orders)
	r.groupByProfits(orders)
	r.groupByEnters(orders)
	r.groupByExits(orders)
	err := r.calcMeasures(30)
	if err != nil {
		log.Warn("calc sharpe/sortino fail", zap.Error(err))
	}
}

func (r *BTResult) textMetrics(orders []*orm.InOutOrder) string {
	var b bytes.Buffer
	table := tablewriter.NewWriter(&b)
	heads := []string{"Metric", "Value"}
	table.SetHeader(heads)
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")
	table.SetAlignment(tablewriter.ALIGN_RIGHT)
	table.Append([]string{"Backtest From", btime.ToDateStr(r.StartMS, "")})
	table.Append([]string{"Backtest To", btime.ToDateStr(r.EndMS, "")})
	table.Append([]string{"Max Open Orders", strconv.Itoa(r.MaxOpenOrders)})
	table.Append([]string{"Total Orders/BarNum", fmt.Sprintf("%v/%v", len(orders), r.BarNum)})
	table.Append([]string{"Total Investment", strconv.FormatFloat(r.TotalInvest, 'f', 0, 64)})
	wallets := biz.GetWallets("")
	finBalance := wallets.AvaLegal(nil)
	table.Append([]string{"Final Balance", strconv.FormatFloat(finBalance, 'f', 0, 64)})
	finWithDraw := wallets.GetWithdrawLegal(nil)
	table.Append([]string{"Final WithDraw", strconv.FormatFloat(finWithDraw, 'f', 0, 64)})
	table.Append([]string{"Absolute Profit", strconv.FormatFloat(r.TotProfit, 'f', 0, 64)})
	totProfitPct := strconv.FormatFloat(r.TotProfitPct, 'f', 1, 64)
	table.Append([]string{"Total Profit %", totProfitPct + "%"})
	table.Append([]string{"Total Fee", strconv.FormatFloat(r.TotFee, 'f', 0, 64)})
	avfProfit := strconv.FormatFloat(r.TotProfitPct*10/float64(len(orders)), 'f', 1, 64)
	table.Append([]string{"Avg Profit ‰", avfProfit + "‰"})
	table.Append([]string{"Total Cost", strconv.FormatFloat(r.TotCost, 'f', 0, 64)})
	avgCost := r.TotCost / float64(len(orders))
	table.Append([]string{"Avg Cost", strconv.FormatFloat(avgCost, 'f', 1, 64)})
	slices.SortFunc(orders, func(a, b *orm.InOutOrder) int {
		return int((a.Profit - b.Profit) * 100)
	})
	if len(orders) > 0 {
		worstVal := strconv.FormatFloat(orders[0].Profit, 'f', 1, 64)
		worstPct := strconv.FormatFloat(orders[0].ProfitRate*100, 'f', 1, 64)
		bestVal := strconv.FormatFloat(orders[len(orders)-1].Profit, 'f', 1, 64)
		bestPct := strconv.FormatFloat(orders[len(orders)-1].ProfitRate*100, 'f', 1, 64)
		table.Append([]string{"Best Order", bestVal + "  " + bestPct + "%"})
		table.Append([]string{"Worst Order", worstVal + "  " + worstPct + "%"})
	}
	table.Append([]string{"Max Assets", strconv.FormatFloat(r.MaxReal, 'f', 1, 64)})
	table.Append([]string{"Min Assets", strconv.FormatFloat(r.MinReal, 'f', 1, 64)})
	drawDownRate := strconv.FormatFloat(r.ShowDrawDownPct, 'f', 1, 64) + "%"
	realDrawDown := strconv.FormatFloat(r.MaxDrawDownPct, 'f', 1, 64) + "%"
	table.Append([]string{"Max DrawDown", fmt.Sprintf("%v / %v", drawDownRate, realDrawDown)})
	sharpeStr := strconv.FormatFloat(r.SharpeRatio, 'f', 2, 64)
	sortinoStr := strconv.FormatFloat(r.SortinoRatio, 'f', 2, 64)
	table.Append([]string{"Sharpe/Sortino", sharpeStr + " / " + sortinoStr})
	table.Render()
	return b.String()
}

func (r *BTResult) groupByPairs(orders []*orm.InOutOrder) {
	groups := groupItems(orders, true, func(od *orm.InOutOrder, i int) string {
		return od.Symbol
	})
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Sharpe > groups[j].Sharpe
	})
	r.PairGrps = groups
}

func textGroupPairs(r *BTResult) string {
	return printGroups(r.PairGrps, "Pair", true, nil, nil)
}

func (r *BTResult) groupByEnters(orders []*orm.InOutOrder) {
	groups := groupItems(orders, true, func(od *orm.InOutOrder, i int) string {
		return od.EnterTag
	})
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Title < groups[j].Title
	})
	r.EnterGrps = groups
}

func textGroupEntTags(r *BTResult) string {
	return printGroups(r.EnterGrps, "Enter Tag", true, nil, nil)
}

func (r *BTResult) groupByExits(orders []*orm.InOutOrder) {
	groups := groupItems(orders, true, func(od *orm.InOutOrder, i int) string {
		return od.ExitTag
	})
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Title < groups[j].Title
	})
	r.ExitGrps = groups
}

func textGroupExitTags(r *BTResult) string {
	return printGroups(r.ExitGrps, "Exit Tag", true, nil, nil)
}

func (r *BTResult) groupByProfits(orders []*orm.InOutOrder) {
	odNum := len(orders)
	if odNum == 0 {
		return
	}
	rates := make([]float64, 0, len(orders))
	for _, od := range orders {
		rates = append(rates, od.ProfitRate)
	}
	var clsNum int
	if odNum > 150 {
		clsNum = min(19, int(math.Round(math.Pow(float64(odNum), 0.5))))
	} else {
		clsNum = int(math.Round(math.Pow(float64(odNum), 0.6)))
	}
	res := utils.KMeansVals(rates, clsNum)
	var grpTitles = make([]string, 0, len(res.Clusters))
	for _, gp := range res.Clusters {
		minPct := strconv.FormatFloat(slices.Min(gp.Items)*100, 'f', 2, 64)
		maxPct := strconv.FormatFloat(slices.Max(gp.Items)*100, 'f', 2, 64)
		grpTitles = append(grpTitles, fmt.Sprintf("%s ~ %s%%", minPct, maxPct))
	}
	groups := groupItems(orders, false, func(od *orm.InOutOrder, i int) string {
		return grpTitles[res.RowGIds[i]]
	})
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Orders[0].ProfitRate < groups[j].Orders[0].ProfitRate
	})
	r.ProfitGrps = groups
}

func textGroupProfitRanges(r *BTResult) string {
	return printGroups(r.ProfitGrps, "Profit Range", false, []string{"Enter Tags", "Exit Tags"}, makeEnterExits)
}

func (r *BTResult) groupByDates(orders []*orm.InOutOrder) {
	units := []string{"1Y", "1Q", "1M", "1w", "1d", "6h", "1h"}
	startMS, endMS := orders[0].EnterAt, orders[len(orders)-1].EnterAt
	var bestTF string
	var bestTFSecs int
	var bestScore float64
	// Find the optimal granularity for grouping
	// 查找分组的最佳粒度
	for _, tf := range units {
		tfSecs := utils2.TFToSecs(tf)
		grpNum := float64(endMS-startMS) / 1000 / float64(tfSecs)
		numPerGp := float64(len(orders)) / grpNum
		score1 := utils.NearScore(grpNum, 18, 2)
		score2 := utils.NearScore(min(numPerGp, 60), 40, 1)
		curScore := score2 * score1
		if curScore > bestScore {
			bestTF = tf
			bestTFSecs = tfSecs
			bestScore = curScore
		}
	}
	if bestTF == "" {
		bestTF = "1d"
		bestTFSecs = utils2.TFToSecs(bestTF)
	}
	tfUnit := bestTF[1]
	groups := groupItems(orders, false, func(od *orm.InOutOrder, i int) string {
		if tfUnit == 'Y' {
			return btime.ToDateStr(od.EnterAt, "2006")
		} else if tfUnit == 'Q' {
			enterMS := utils2.AlignTfMSecs(od.EnterAt, int64(bestTFSecs*1000))
			return btime.ToDateStr(enterMS, "2006-01")
		} else if tfUnit == 'M' {
			return btime.ToDateStr(od.EnterAt, "2006-01")
		} else if tfUnit == 'd' || tfUnit == 'w' {
			enterMS := utils2.AlignTfMSecs(od.EnterAt, int64(bestTFSecs*1000))
			return btime.ToDateStr(enterMS, "2006-01-02")
		} else {
			return btime.ToDateStr(od.EnterAt, "2006-01-02 15")
		}
	})
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Title < groups[j].Title
	})
	r.DateGrps = groups
}

func textGroupDays(r *BTResult) string {
	return printGroups(r.DateGrps, "Date", false, []string{"Enter Tags", "Exit Tags"}, makeEnterExits)
}

func makeEnterExits(orders []*orm.InOutOrder) []string {
	enters := make(map[string]int)
	exits := make(map[string]int)
	for _, od := range orders {
		if num, ok := enters[od.EnterTag]; ok {
			enters[od.EnterTag] = num + 1
		} else {
			enters[od.EnterTag] = 1
		}
		if num, ok := exits[od.ExitTag]; ok {
			exits[od.ExitTag] = num + 1
		} else {
			exits[od.ExitTag] = 1
		}
	}
	entList := make([]string, 0, len(enters))
	exitList := make([]string, 0, len(enters))
	for k, v := range enters {
		entList = append(entList, fmt.Sprintf("%s/%v", k, v))
	}
	for k, v := range exits {
		exitList = append(exitList, fmt.Sprintf("%s/%v", k, v))
	}
	return []string{
		strings.Join(entList, " "),
		strings.Join(exitList, " "),
	}
}

func groupItems(orders []*orm.InOutOrder, measure bool, getTag func(od *orm.InOutOrder, i int) string) []*RowItem {
	if len(orders) == 0 {
		return nil
	}
	groups := make(map[string]*RowItem)
	for i, od := range orders {
		tag := getTag(od, i)
		sta, ok := groups[tag]
		duration := max(0, int((od.ExitAt-od.EnterAt)/1000))
		isWin := od.Profit >= 0
		if !ok {
			sta = &RowItem{
				Title: tag,
				RowPart: RowPart{
					ProfitSum:    od.Profit,
					ProfitPctSum: od.ProfitRate,
					CostSum:      od.EnterCost() / od.Leverage,
					Durations:    []int{duration},
					Orders:       make([]*orm.InOutOrder, 0, 8),
				},
			}
			sta.Orders = append(sta.Orders, od)
			if isWin {
				sta.WinCount = 1
			}
			groups[tag] = sta
		} else {
			if isWin {
				sta.WinCount += 1
			}
			sta.ProfitSum += od.Profit
			sta.ProfitPctSum += od.ProfitRate
			sta.CostSum += od.EnterCost() / od.Leverage
			sta.Durations = append(sta.Durations, duration)
			sta.Orders = append(sta.Orders, od)
		}
	}
	if measure {
		// 分30份采样计算指标，太大的话会导致指标偏小
		for _, gp := range groups {
			sharpe, sortino, err := measurePerformance(gp.Orders, 30)
			if err != nil {
				log.Warn("calc measure fail", zap.Error(err))
			} else {
				gp.Sharpe = sharpe
				gp.Sortino = sortino
			}
		}
	}
	return utils.ValsOfMap(groups)
}

func printGroups(groups []*RowItem, title string, measure bool, extHeads []string, prcGrp func([]*orm.InOutOrder) []string) string {
	var b bytes.Buffer
	table := tablewriter.NewWriter(&b)
	heads := []string{title, "Count", "Avg Profit %", "Tot Profit %", "Sum Profit", "Duration(h'm)", "Win Rate"}
	if measure {
		heads = append(heads, "Sharpe/Sortino")
	}
	if len(extHeads) > 0 {
		heads = append(heads, extHeads...)
	}
	table.SetHeader(heads)
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")
	table.SetAlignment(tablewriter.ALIGN_RIGHT)
	for _, sta := range groups {
		grpCount := len(sta.Orders)
		numText := strconv.Itoa(grpCount)
		avgProfit := strconv.FormatFloat(sta.ProfitPctSum*100/float64(grpCount), 'f', 2, 64)
		totProfit := strconv.FormatFloat(sta.ProfitSum*100/sta.CostSum, 'f', 2, 64)
		sumProfit := strconv.FormatFloat(sta.ProfitSum, 'f', 2, 64)
		duraText := kMeansDurations(sta.Durations, 3)
		winRate := strconv.FormatFloat(float64(sta.WinCount)*100/float64(grpCount), 'f', 1, 64) + "%"
		row := []string{sta.Title, numText, avgProfit, totProfit, sumProfit, duraText, winRate}
		if measure {
			sharpeStr := strconv.FormatFloat(sta.Sharpe, 'f', 2, 64)
			sortinoStr := strconv.FormatFloat(sta.Sortino, 'f', 2, 64)
			row = append(row, sharpeStr+" / "+sortinoStr)
		}
		if prcGrp != nil {
			cols := prcGrp(sta.Orders)
			row = append(row, cols...)
		}
		table.Append(row)
	}
	table.Render()
	return b.String()
}

func measurePerformance(ods []*orm.InOutOrder, num int) (float64, float64, *errs.Error) {
	if len(ods) == 0 {
		return 0, 0, nil
	}
	sort.Slice(ods, func(i, j int) bool {
		return ods[i].ExitAt < ods[j].ExitAt
	})
	var startMs = ods[0].EnterAt
	var stopMs = ods[len(ods)-1].ExitAt
	stepMs := (stopMs - startMs) / int64(num)
	curEnd := startMs + stepMs
	profit, cost := float64(0), float64(0)
	allProfit, allCost := float64(0), float64(0)
	returns := make([]float64, 0)
	for _, od := range ods {
		for od.ExitAt > curEnd {
			if cost > 0 {
				returns = append(returns, profit/cost)
			}
			profit, cost = 0, 0
			curEnd += stepMs
		}
		curCost := od.EnterCost() / od.Leverage
		cost += curCost
		profit += od.Profit
		allCost += curCost
		allProfit += od.Profit
	}
	if cost > 0 {
		returns = append(returns, profit/cost)
	}
	if len(returns) <= 2 {
		return 0, 0, nil
	}
	return calcMeasures(returns)
}

func calcMeasures(returns []float64) (float64, float64, *errs.Error) {
	sharpeFlt, err := utils.SharpeRatio(returns, 0)
	if err != nil {
		return 0, 0, errs.New(errs.CodeRunTime, err)
	}
	sortineFlt, err := utils.SortinoRatio(returns, 0)
	if err != nil {
		if !errors.Is(err, utils.ErrNoNegativeResults) {
			return sharpeFlt, 0, errs.New(errs.CodeRunTime, err)
		}
		sortineFlt = math.Inf(1)
	}
	return sharpeFlt, sortineFlt, nil
}

func kMeansDurations(durations []int, num int) string {
	slices.Sort(durations)
	diffNum := 1
	for i, val := range durations[1:] {
		if val != durations[i] {
			diffNum += 1
		}
	}
	if diffNum < num {
		if len(durations) == 0 {
			return ""
		}
		num = diffNum
	}
	var d = make([]float64, 0, len(durations))
	for _, dura := range durations {
		d = append(d, float64(dura))
	}
	var res = utils.KMeansVals(d, num)
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, grp := range res.Clusters {
		grpNum := len(grp.Items)
		var coord int
		if grpNum == 1 {
			coord = int(math.Round(grp.Items[0]))
		} else {
			coord = int(math.Round(grp.Center))
		}
		if coord < 60 {
			b.WriteString(strconv.Itoa(coord))
			b.WriteString("s")
		} else {
			mins := coord / 60
			hours := mins / 60
			lmins := mins % 60
			b.WriteString(strconv.Itoa(hours))
			if hours <= 99 {
				b.WriteString("'")
				b.WriteString(strconv.Itoa(lmins))
			}
		}
		b.WriteString("/")
		b.WriteString(strconv.Itoa(grpNum))
		b.WriteString("  ")
	}
	return b.String()
}

func (r *BTResult) dumpOrders(orders []*orm.InOutOrder) {
	sort.Slice(orders, func(i, j int) bool {
		var a, b = orders[i], orders[j]
		if a.EnterAt != b.EnterAt {
			return a.EnterAt < b.EnterAt
		}
		return a.Symbol < b.Symbol
	})
	taskId := orm.GetTaskID("")
	file, err_ := os.Create(fmt.Sprintf("%s/orders_%v.csv", r.OutDir, taskId))
	if err_ != nil {
		log.Error("create orders.csv fail", zap.Error(err_))
		return
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	heads := []string{"sid", "symbol", "timeframe", "direction", "leverage", "entAt", "entTag", "entPrice",
		"entAmount", "entCost", "entFee", "exitAt", "exitTag", "exitPrice", "exitAmount", "exitGot",
		"exitFee", "max_draw_down", "profitRate", "profit"}
	if err_ = writer.Write(heads); err_ != nil {
		log.Error("write orders.csv fail", zap.Error(err_))
		return
	}
	colNum := len(heads)
	for _, od := range orders {
		row := make([]string, colNum)
		row[0] = fmt.Sprintf("%v", od.Sid)
		row[1] = od.Symbol
		row[2] = od.Timeframe
		row[3] = "long"
		if od.Short {
			row[3] = "short"
		}
		row[4] = fmt.Sprintf("%v", od.Leverage)
		row[5] = btime.ToDateStr(od.EnterAt, "")
		row[6] = od.EnterTag
		if od.Enter != nil {
			row[7], row[8], row[9], row[10] = calcExOrder(od.Enter)
		}
		row[11] = btime.ToDateStr(od.ExitAt, "")
		row[12] = od.ExitTag
		if od.Exit != nil {
			row[13], row[14], row[15], row[16] = calcExOrder(od.Exit)
		}
		row[17] = strconv.FormatFloat(od.MaxDrawDown, 'f', 4, 64)
		row[18] = strconv.FormatFloat(od.ProfitRate, 'f', 4, 64)
		row[19] = strconv.FormatFloat(od.Profit, 'f', 8, 64)
		if err_ = writer.Write(row); err_ != nil {
			log.Error("write orders.csv fail", zap.Error(err_))
			return
		}
	}
}

func calcExOrder(od *orm.ExOrder) (string, string, string, string) {
	price := od.Average
	if price == 0 {
		price = od.Price
	}
	exitGot := price * od.Filled
	priceStr := strconv.FormatFloat(price, 'f', 8, 64)
	amtStr := strconv.FormatFloat(od.Filled, 'f', 8, 64)
	valStr := strconv.FormatFloat(exitGot, 'f', 4, 64)
	feeStr := strconv.FormatFloat(od.Fee, 'f', 8, 64)
	return priceStr, amtStr, valStr, feeStr
}

func (r *BTResult) dumpConfig() {
	data, err := config.DumpYaml()
	if err != nil {
		log.Error("marshal config as yaml fail", zap.Error(err))
		return
	}
	taskId := orm.GetTaskID("")
	outName := fmt.Sprintf("%s/config_%v.yml", r.OutDir, taskId)
	err_ := os.WriteFile(outName, data, 0644)
	if err_ != nil {
		log.Error("save yaml to file fail", zap.Error(err_))
	}
}

func (r *BTResult) dumpStrategy() {
	stratDir := config.GetStratDir()
	for name := range core.StgPairTfs {
		dname := strings.Split(name, ":")[0]
		srcDir := fmt.Sprintf("%s/%s", stratDir, dname)
		tgtDir := fmt.Sprintf("%s/strat_%s", r.OutDir, dname)
		err_ := utils.CopyDir(srcDir, tgtDir)
		if err_ != nil {
			log.Error("backup strat fail", zap.String("name", name), zap.Error(err_))
		}
	}
}

func (r *BTResult) dumpStratOutputs() {
	groups := make(map[string][]string)
	for _, items := range strat.PairStags {
		for _, stgy := range items {
			if len(stgy.Outputs) == 0 {
				continue
			}
			rows, _ := groups[stgy.Name]
			groups[stgy.Name] = append(rows, stgy.Outputs...)
			stgy.Outputs = nil
		}
	}
	for name, rows := range groups {
		name = strings.ReplaceAll(name, ":", "_")
		outPath := fmt.Sprintf("%s/%s.log", r.OutDir, name)
		file, err := os.Create(outPath)
		if err != nil {
			log.Error("create strategy output file fail", zap.String("name", name), zap.Error(err))
			continue
		}
		_, err = file.WriteString(strings.Join(rows, "\n"))
		if err != nil {
			log.Error("write strategy output fail", zap.String("name", name), zap.Error(err))
		}
		err = file.Close()
		if err != nil {
			log.Error("close strategy output fail", zap.String("name", name), zap.Error(err))
		}
	}
}

//go:embed btgraph.html
var btGraphData []byte

func (r *BTResult) dumpGraph() {
	tplPath := fmt.Sprintf("%s/btgraph.html", config.GetDataDir())
	fileData, err_ := os.ReadFile(tplPath)
	if err_ != nil {
		log.Warn("btgraph.html load fail, use default", zap.String("path", tplPath), zap.Error(err_))
		fileData = btGraphData
	}
	content := string(fileData)
	content = strings.Replace(content, "$title", "Real-time Assets/Balances/Unrealized P&L/Withdrawals/Concurrent Orders", 1)
	items := map[string]interface{}{
		"\"$labels\"":    r.Plots.Labels,
		"\"$odNum\"":     r.Plots.OdNum,
		"\"$real\"":      r.Plots.Real,
		"\"$available\"": r.Plots.Available,
		"\"$profit\"":    r.Plots.UnrealizedPOL,
		"\"$withdraw\"":  r.Plots.WithDraw,
	}
	for k, v := range items {
		var b strings.Builder
		if labels, ok := v.([]string); ok {
			for _, it := range labels {
				b.WriteString("\"")
				b.WriteString(it)
				b.WriteString("\",")
			}
		} else if vals, ok := v.([]float64); ok {
			for _, it := range vals {
				b.WriteString(strconv.FormatFloat(it, 'f', 2, 64))
				b.WriteString(",")
			}
		} else if vals, ok := v.([]int); ok {
			for _, it := range vals {
				b.WriteString(strconv.Itoa(it))
				b.WriteString(",")
			}
		} else {
			log.Error("unsupport plot type", zap.String("key", k), zap.String("type", fmt.Sprintf("%T", v)))
		}
		content = strings.Replace(content, k, b.String(), 1)
	}
	taskId := orm.GetTaskID("")
	outPath := fmt.Sprintf("%s/assets_%v.html", r.OutDir, taskId)
	err_ = os.WriteFile(outPath, []byte(content), 0644)
	if err_ != nil {
		log.Error("save assets.html fail", zap.Error(err_))
	}
}

func (p *PlotData) calcDrawDown() float64 {
	var drawDownRate, maxReal float64
	if len(p.Real) > 0 {
		reals := p.Real
		maxReal = reals[0]
		for _, val := range reals {
			if val > maxReal {
				maxReal = val
			} else {
				curDown := math.Abs(val/maxReal - 1)
				if curDown > drawDownRate {
					drawDownRate = curDown
				}
			}
		}
	}
	return drawDownRate
}

func (r *BTResult) calcMeasures(num int) *errs.Error {
	p := r.Plots
	if len(p.Real) <= 1 {
		return nil
	}
	step := len(p.Real) / num
	prevVal := p.Real[0]
	inReturns := make([]float64, 0, num)
	for i := step; i < len(p.Real); i += step {
		curVal := p.Real[i]
		inReturns = append(inReturns, (curVal-prevVal)/prevVal)
		prevVal = curVal
	}
	if len(inReturns) < num {
		last := p.Real[len(p.Real)-1]
		inReturns = append(inReturns, (last-prevVal)/prevVal)
	}
	sharpe, sortino, err := calcMeasures(inReturns)
	if err != nil {
		return err
	}
	r.SharpeRatio = sharpe
	r.SortinoRatio = sortino
	return nil
}

func (r *BTResult) Score() float64 {
	var score float64
	if r.TotProfitPct <= 0 {
		score = r.TotProfitPct
	} else {
		// 盈利时返回无回撤收益率
		score = r.TotProfitPct * math.Pow(1-r.MaxDrawDownPct/100, 1.5)
	}
	return score
}

func (r *BTResult) dumpDetail(outPath string) {
	if outPath == "" {
		taskId := orm.GetTaskID("")
		outPath = fmt.Sprintf("%s/detail_%v.json", r.OutDir, taskId)
	}
	var w = bytes.NewBuffer(nil)
	var enc = sonic.Config{EncodeNullForInfOrNan: true}.Froze().NewEncoder(w)
	err_ := enc.Encode(r)
	if err_ != nil {
		log.Error("marshal backtest detail fail", zap.Error(err_))
		return
	}
	err_ = os.WriteFile(outPath, w.Bytes(), 0644)
	if err_ != nil {
		log.Error("write backtest detail fail", zap.Error(err_))
	}
}

func selectPairs(r *BTResult, name string) []string {
	fn, ok := PairPickers[name]
	if !ok {
		log.Warn("PairPickers not found", zap.String("name", name))
		return nil
	}
	return fn(r)
}

func parseBtResult(path string) (*BTResult, *errs.Error) {
	data, err_ := os.ReadFile(path)
	if err_ != nil {
		return nil, errs.New(errs.CodeIOReadFail, err_)
	}
	var res = BTResult{}
	err_ = utils2.Unmarshal(data, &res)
	if err_ != nil {
		return nil, errs.New(errs.CodeUnmarshalFail, err_)
	}
	return &res, nil
}
