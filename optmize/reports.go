package optmize

import (
	"bytes"
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/muesli/clusters"
	"github.com/muesli/kmeans"
	"github.com/olekukonko/tablewriter"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type BTResult struct {
	MaxOpenOrders int
	MinBalance    float64
	MaxBalance    float64
	BarNum        int
	Plots         PlotData
	StartMS       int64
	EndMS         int64
	PlotEvery     int
	TotalInvest   float64
}

type PlotData struct {
	Real      []TextFloat
	Available []TextFloat
	Profit    []TextFloat
	WithDraw  []TextFloat
}

type TextFloat struct {
	Text string
	Val  float64
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

func (r BTResult) printBtResult() {
	orders := orm.HistODs
	var tblText string
	if len(orders) > 0 {
		items := []struct {
			Title  string
			Handle func([]*orm.InOutOrder) string
		}{
			{Title: " Pair Profits ", Handle: textGroupPairs},
			{Title: " Day Profits ", Handle: textGroupDays},
			{Title: " Profit Ranges ", Handle: textGroupProfitRanges},
			{Title: " Enter Tag ", Handle: textGroupEntTags},
			{Title: " Exit Tag ", Handle: textGroupExitTags},
		}
		for _, item := range items {
			tblText = item.Handle(orders)
			if tblText != "" {
				width := strings.Index(tblText, "\n")
				head := utils.PadCenter(item.Title, width, "=")
				fmt.Println(head)
				fmt.Println(tblText)
			}
		}
	} else {
		fmt.Println("No Orders Found")
	}
	table := tablewriter.NewWriter(os.Stdout)
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
	finBalance := biz.Wallets.AvaLegal(nil)
	table.Append([]string{"Final Balance", strconv.FormatFloat(finBalance, 'f', 0, 64)})
	finWithDraw := biz.Wallets.GetWithdrawLegal(nil)
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
	avfProfit := strconv.FormatFloat(totProfitPctVal*1000/float64(len(orders)), 'f', 1, 64)
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
	table.Append([]string{"Max Balance", strconv.FormatFloat(r.MaxBalance, 'f', 1, 64)})
	table.Append([]string{"Min Balance", strconv.FormatFloat(r.MinBalance, 'f', 1, 64)})
	table.Render()
}

func textGroupPairs(orders []*orm.InOutOrder) string {
	groups := groupItems(orders, func(od *orm.InOutOrder, i int) string {
		return od.Symbol
	})
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Title < groups[j].Title
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
	res := kMeansVals(rates, clsNum)
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
	groups := groupItems(orders, func(od *orm.InOutOrder, i int) string {
		return btime.ToDateStr(od.EnterAt, "2006-01-02")
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
	heads := []string{title, "Count", "Avg Profit %", "Tot Profit %", "Sum Profit", "Duration", "Win Rate"}
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

type Cluster struct {
	Center float64
	Items  []float64
}

type ClusterRes struct {
	Clusters []Cluster
	RowGIds  []int
}

func kMeansVals(vals []float64, num int) *ClusterRes {
	// 输入值域在0~1之间
	minVal := slices.Min(vals)
	scale := 1 / (slices.Max(vals) - minVal)
	if len(vals) == 1 {
		scale = 1 / minVal
	}
	offset := 0 - minVal*scale
	var d clusters.Observations
	for _, val := range vals {
		d = append(d, clusters.Coordinates{val*scale + offset})
	}
	// 进行聚类
	km := kmeans.New()
	groups, err_ := km.Partition(d, num)
	if err_ != nil {
		return nil
	}
	slices.SortFunc(groups, func(a, b clusters.Cluster) int {
		return int((a.Center[0] - b.Center[0]) * 1000)
	})
	// 生成返回结果
	resList := make([]Cluster, 0, len(groups))
	seps := make([]float64, 0, len(groups))
	for i, group := range groups {
		var center = (group.Center[0] - offset) / scale
		var items = make([]float64, 0, len(group.Observations))
		for _, it := range group.Observations {
			coords := it.Coordinates()
			items = append(items, (coords[0]-offset)/scale)
		}
		resList = append(resList, Cluster{
			Center: center,
			Items:  items,
		})
		curMax := slices.Max(items)
		curMin := slices.Min(items)
		if len(seps) > 0 {
			seps[i-1] = (seps[i-1] + curMin) / 2
		}
		seps = append(seps, curMax)
	}
	// 计算每个项所属的分组
	rowGids := make([]int, 0, len(vals))
	for _, v := range vals {
		gid := len(groups) - 1
		for i, end := range seps {
			if v < end {
				gid = i
				break
			}
		}
		rowGids = append(rowGids, gid)
	}
	return &ClusterRes{
		Clusters: resList,
		RowGIds:  rowGids,
	}
}

func kMeansDurations(durations []int, num int) string {
	if len(durations) < num {
		if len(durations) == 0 {
			return ""
		}
		num = len(durations)
	}
	var d = make([]float64, 0, len(durations))
	for _, dura := range durations {
		d = append(d, float64(dura))
	}
	var res = kMeansVals(d, num)
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, grp := range res.Clusters {
		coord := int(math.Round(grp.Center))
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
		b.WriteString(strconv.Itoa(len(grp.Items)))
		b.WriteString("  ")
	}
	return b.String()
}
