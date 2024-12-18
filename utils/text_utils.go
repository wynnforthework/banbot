package utils

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/banbox/banbot/core"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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

func UniqueItems[T comparable](arr []T) ([]T, []T) {
	var res = make([]T, 0, len(arr))
	var has = make(map[T]bool)
	var dups = make([]T, 0, len(arr)/10)
	for _, it := range arr {
		if _, ok := has[it]; ok {
			dups = append(dups, it)
			continue
		}
		res = append(res, it)
		has[it] = true
	}
	return res, dups
}

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func RandomStr(length int) string {
	b := make([]byte, length)
	for i := range b {
		randomIndex, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			panic(err)
		}
		b[i] = charset[randomIndex.Int64()]
	}
	return string(b)
}

// IsTextContent 检查字节数组是否可能是文本内容
// 返回 true 表示可能是文本内容，false 表示可能是二进制内容
func IsTextContent(data []byte) bool {
	if len(data) == 0 {
		return true
	}

	if utf8.Valid(data) {
		return true
	}

	// 检查常见的文本字符
	textChars := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*()-_=+[]{}|;:,.<>?/\n\r\t ")

	// 统计文本字符的比例
	textCharCount := 0
	for _, b := range data {
		if bytes.Contains(textChars, []byte{b}) {
			textCharCount++
		}
	}

	// 如果文本字符占比超过 70%，认为是文本内容
	textRatio := float64(textCharCount) / float64(len(data))
	if textRatio > 0.7 {
		return true
	}

	// 检查是否包含过多的控制字符或 null 字符
	controlCharCount := 0
	for _, b := range data {
		if b < 32 && !bytes.Contains([]byte{'\n', '\r', '\t'}, []byte{b}) {
			controlCharCount++
		}
	}

	// 如果控制字符占比超过 10%，认为是二进制内容
	controlRatio := float64(controlCharCount) / float64(len(data))
	if controlRatio > 0.1 {
		return false
	}

	return true
}
