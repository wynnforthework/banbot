package core

import (
	"fmt"
	"github.com/banbox/banexg/log"
	"regexp"
	"slices"
	"strings"
)

/*
GroupStagyPairs
【Stagy_TimeFrame】
Quote: Base1 Base2 ...
*/
func GroupStagyPairs() map[string]map[string][]string {
	groups := make(map[string][]string)
	for stagy, pairMap := range StgPairTfs {
		for pair, tf := range pairMap {
			key := fmt.Sprintf("%s_%s", stagy, tf)
			arr, _ := groups[key]
			groups[key] = append(arr, pair)
		}
	}
	res := make(map[string]map[string][]string)
	for key, arr := range groups {
		slices.Sort(arr)
		quoteMap := make(map[string][]string)
		for _, pair := range arr {
			baseCode, quoteCode, _, _ := SplitSymbol(pair)
			baseList, _ := quoteMap[quoteCode]
			quoteMap[quoteCode] = append(baseList, baseCode)
		}
		for quote, baseList := range quoteMap {
			slices.Sort(baseList)
			quoteMap[quote] = baseList
		}
		res[key] = quoteMap
	}
	return res
}

/*
PrintStagyGroups
从core.StgPairTfs输出策略+时间周期的币种信息到控制台
*/
func PrintStagyGroups() {
	items := GroupStagyPairs()
	var b strings.Builder
	for key, quoteMap := range items {
		b.WriteString(fmt.Sprintf("【%s】\n", key))
		for quoteCode, arr := range quoteMap {
			baseStr := strings.Join(arr, " ")
			b.WriteString(fmt.Sprintf("%s(%d): %s\n", quoteCode, len(arr), baseStr))
		}
	}
	log.Info("group pairs by stagy_tf:\n" + b.String())
}

var (
	reCoinSplit = regexp.MustCompile("[/:-]")
)

/*
SplitSymbol
返回：Base，Quote，Settle，Identifier
*/
func SplitSymbol(pair string) (string, string, string, string) {
	parts := reCoinSplit.Split(pair, -1)
	settle, ident := "", ""
	if len(parts) > 2 {
		settle = parts[2]
	}
	if len(parts) > 3 {
		ident = parts[3]
	}
	return parts[0], parts[1], settle, ident
}
