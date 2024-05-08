package optmize

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strategy"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/log"
	"github.com/olekukonko/tablewriter"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"math"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type BTResult struct {
	MaxOpenOrders  int
	MinReal        float64
	MaxReal        float64 // 最大资产
	MaxDrawDownPct float64 // 最大回撤百分比
	BarNum         int
	TimeNum        int
	lastTime       int64 // 上次bar的时间戳
	Plots          *PlotData
	StartMS        int64
	EndMS          int64
	PlotEvery      int
	TotalInvest    float64
	OutDir         string
}

type PlotData struct {
	Labels        []string
	OdNum         []int
	Real          []float64
	Available     []float64
	UnrealizedPOL []float64
	WithDraw      []float64
	tmpOdNum      int
}

type RowPart struct {
	WinCount     int
	ProfitSum    float64
	ProfitPctSum float64
	CostSum      float64
	Durations    []int
	Orders       []*orm.InOutOrder
}

type RowItem struct {
	Title string
	RowPart
}

func NewBTResult() *BTResult {
	res := &BTResult{
		Plots:     &PlotData{},
		PlotEvery: 1,
	}
	return res
}

func (r *BTResult) printBtResult() {
	core.DumpPerfs(r.OutDir)

	orders := orm.HistODs
	var b strings.Builder
	var tblText string
	if len(orders) > 0 {
		items := []struct {
			Title  string
			Handle func([]*orm.InOutOrder) string
		}{
			{Title: " Pair Profits ", Handle: textGroupPairs},
			{Title: " Date Profits ", Handle: textGroupDays},
			{Title: " Profit Ranges ", Handle: textGroupProfitRanges},
			{Title: " Enter Tag ", Handle: textGroupEntTags},
			{Title: " Exit Tag ", Handle: textGroupExitTags},
		}
		for _, item := range items {
			tblText = item.Handle(orders)
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
	b.WriteString(r.textMetrics(orders))
	log.Info("BackTest Reports:\n" + b.String())

	r.dumpOrders(orders)

	r.dumpConfig()

	r.dumpStrategy()

	r.dumpStagyOutputs()

	r.dumpGraph()
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
	sumProfit := float64(0)
	sumFee := float64(0)
	sumCost := float64(0)
	for _, od := range orders {
		sumProfit += od.Profit
		sumFee += od.Enter.Fee
		if od.Exit != nil {
			sumFee += od.Exit.Fee
		}
		sumCost += od.EnterCost()
	}
	table.Append([]string{"Absolute Profit", strconv.FormatFloat(sumProfit, 'f', 0, 64)})
	totProfitPctVal := sumProfit * 100 / r.TotalInvest
	totProfitPct := strconv.FormatFloat(totProfitPctVal, 'f', 1, 64)
	table.Append([]string{"Total Profit %", totProfitPct + "%"})
	table.Append([]string{"Total Fee", strconv.FormatFloat(sumFee, 'f', 0, 64)})
	avfProfit := strconv.FormatFloat(totProfitPctVal*10/float64(len(orders)), 'f', 1, 64)
	table.Append([]string{"Avg Profit ‰", avfProfit + "‰"})
	table.Append([]string{"Total Cost", strconv.FormatFloat(sumCost, 'f', 0, 64)})
	avgCost := sumCost / float64(len(orders))
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
	if r.MinReal > r.MaxReal {
		r.MinReal = r.MaxReal
	}
	table.Append([]string{"Min Assets", strconv.FormatFloat(r.MinReal, 'f', 1, 64)})
	// 计算图表上的最大回撤
	drawDownRate := strconv.FormatFloat(r.Plots.calcDrawDown()*100, 'f', 1, 64) + "%"
	realDrawDown := strconv.FormatFloat(r.MaxDrawDownPct, 'f', 1, 64) + "%"
	table.Append([]string{"Max DrawDown", fmt.Sprintf("%v / %v", drawDownRate, realDrawDown)})
	table.Render()
	return b.String()
}

func textGroupPairs(orders []*orm.InOutOrder) string {
	groups := groupItems(orders, func(od *orm.InOutOrder, i int) string {
		return od.Symbol
	})
	sort.Slice(groups, func(i, j int) bool {
		return len(groups[i].Orders) > len(groups[j].Orders)
	})
	return printGroups(groups, "Pair", nil, nil)
}

func textGroupEntTags(orders []*orm.InOutOrder) string {
	groups := groupItems(orders, func(od *orm.InOutOrder, i int) string {
		return od.EnterTag
	})
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Title < groups[j].Title
	})
	return printGroups(groups, "Enter Tag", nil, nil)
}

func textGroupExitTags(orders []*orm.InOutOrder) string {
	groups := groupItems(orders, func(od *orm.InOutOrder, i int) string {
		return od.ExitTag
	})
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Title < groups[j].Title
	})
	return printGroups(groups, "Exit Tag", nil, nil)
}

func textGroupProfitRanges(orders []*orm.InOutOrder) string {
	odNum := len(orders)
	if odNum == 0 {
		return ""
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
	groups := groupItems(orders, func(od *orm.InOutOrder, i int) string {
		return grpTitles[res.RowGIds[i]]
	})
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Orders[0].ProfitRate < groups[j].Orders[0].ProfitRate
	})
	return printGroups(groups, "Profit Range", []string{"Enter Tags", "Exit Tags"}, makeEnterExits)
}

func textGroupDays(orders []*orm.InOutOrder) string {
	units := []string{"1M", "1w", "1d", "6h", "1h"}
	startMS, endMS := orders[0].EnterAt, orders[len(orders)-1].EnterAt
	var bestTF string
	var bestTFSecs int
	var bestScore float64
	// 查找分组的最佳粒度
	for _, tf := range units {
		tfSecs := utils.TFToSecs(tf)
		grpNum := float64(endMS-startMS) / 1000 / float64(tfSecs)
		numPerGp := float64(len(orders)) / grpNum
		score1 := utils.MaxToZero(grpNum, 15)
		score2 := utils.MaxToZero(numPerGp, 40)
		curScore := score2 * score1
		if curScore > bestScore {
			bestTF = tf
			bestTFSecs = tfSecs
			bestScore = curScore
		}
	}
	if bestTF == "" {
		bestTF = "1d"
		bestTFSecs = utils.TFToSecs(bestTF)
	}
	tfUnit := bestTF[1]
	groups := groupItems(orders, func(od *orm.InOutOrder, i int) string {
		if tfUnit == 'M' {
			return btime.ToDateStr(od.EnterAt, "2006-01")
		} else if tfUnit == 'd' || tfUnit == 'w' {
			enterMS := utils.AlignTfMSecs(od.EnterAt, int64(bestTFSecs*1000))
			return btime.ToDateStr(enterMS, "2006-01-02")
		} else {
			return btime.ToDateStr(od.EnterAt, "2006-01-02 15")
		}
	})
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Title < groups[j].Title
	})
	return printGroups(groups, "Date", []string{"Enter Tags", "Exit Tags"}, makeEnterExits)
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

func groupItems(orders []*orm.InOutOrder, getTag func(od *orm.InOutOrder, i int) string) []*RowItem {
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
					CostSum:      od.EnterCost(),
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
			sta.CostSum += od.EnterCost()
			sta.Durations = append(sta.Durations, duration)
			sta.Orders = append(sta.Orders, od)
		}
	}
	return utils.ValsOfMap(groups)
}

func printGroups(groups []*RowItem, title string, extHeads []string, prcGrp func([]*orm.InOutOrder) []string) string {
	var b bytes.Buffer
	table := tablewriter.NewWriter(&b)
	heads := []string{title, "Count", "Avg Profit %", "Tot Profit %", "Sum Profit", "Duration(h'm)", "Win Rate"}
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
		if prcGrp != nil {
			cols := prcGrp(sta.Orders)
			row = append(row, cols...)
		}
		table.Append(row)
	}
	table.Render()
	return b.String()
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
			b.WriteString("'")
			b.WriteString(strconv.Itoa(lmins))
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
		"entAmount", " entCost", "entFee", "exitAt", "exitTag", "exitPrice", "exitAmount", "exitGot",
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
	itemMap := make(map[string]interface{})
	t := reflect.TypeOf(config.Data)
	v := reflect.ValueOf(config.Data)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		val := v.Field(i)
		itemMap[tag] = val.Interface()
	}
	data, err_ := yaml.Marshal(&itemMap)
	if err_ != nil {
		log.Error("marshal config as yaml fail", zap.Error(err_))
		return
	}
	taskId := orm.GetTaskID("")
	outName := fmt.Sprintf("%s/config_%v.yml", r.OutDir, taskId)
	err_ = os.WriteFile(outName, data, 0644)
	if err_ != nil {
		log.Error("save yaml to file fail", zap.Error(err_))
	}
}

func (r *BTResult) dumpStrategy() {
	stagyDir := config.GetStagyDir()
	for name := range core.StgPairTfs {
		dname := strings.Split(name, ":")[0]
		srcDir := fmt.Sprintf("%s/%s", stagyDir, dname)
		tgtDir := fmt.Sprintf("%s/stagy_%s", r.OutDir, dname)
		err_ := utils.CopyDir(srcDir, tgtDir)
		if err_ != nil {
			log.Error("backup stagy fail", zap.String("name", name), zap.Error(err_))
		}
	}
}

func (r *BTResult) dumpStagyOutputs() {
	for name := range strategy.Versions {
		stgy := strategy.Get(name)
		if len(stgy.Outputs) == 0 {
			continue
		}
		outPath := fmt.Sprintf("%s/%s.log", r.OutDir, name)
		file, err := os.Create(outPath)
		if err != nil {
			log.Error("create strategy output file fail", zap.String("name", name), zap.Error(err))
			continue
		}
		_, err = file.WriteString(strings.Join(stgy.Outputs, "\n"))
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
	content = strings.Replace(content, "$title", "实时资产/余额/未实现盈亏/提现/并发订单数", 1)
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
