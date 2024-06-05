package utils

import (
	"fmt"
	"github.com/banbox/banbot/core"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"sort"
	"strconv"
	"strings"
)

func SnakeToCamel(input string) string {
	parts := strings.Split(input, "_")
	caser := cases.Title(language.English)
	for i, text := range parts {
		parts[i] = caser.String(text)
	}
	return strings.Join(parts, "")
}

func PadCenter(s string, width int, padText string) string {
	// 计算原始字符串的长度
	strLen := len(s)

	if strLen >= width {
		// 如果字符串长度大于等于指定宽度，直接输出
		return s
	}

	// 计算两边应填充的总长度
	paddingTotal := width - strLen
	// 计算左侧填充长度
	leftPadding := paddingTotal / 2
	// 计算右侧填充长度
	rightPadding := paddingTotal - leftPadding

	// 构造左侧填充字符串
	left := strings.Repeat(padText, leftPadding)
	// 构造右侧填充字符串
	right := strings.Repeat(padText, rightPadding)

	// 输出拼接后的字符串
	return left + s + right
}

func MapToStr(m map[string]float64) (string, int) {
	var b strings.Builder
	arr := make([]*core.StrVal, 0, len(m))
	for k, v := range m {
		arr = append(arr, &core.StrVal{Str: k, Val: v})
	}
	sort.Slice(arr, func(i, j int) bool {
		return arr[i].Str < arr[j].Str
	})
	numLen := 0
	for i, p := range arr {
		if i > 0 {
			b.WriteString(", ")
		}
		valStr := strconv.FormatFloat(p.Val, 'f', 2, 64)
		b.WriteString(fmt.Sprintf("%s: %s", p.Str, valStr))
		numLen += len(valStr)
	}
	return b.String(), numLen
}
