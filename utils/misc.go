package utils

import (
	"strings"
)

/*
SplitSolid 字符串分割，忽略返回结果中的空字符串
*/
func SplitSolid(text string, sep string) []string {
	arr := strings.Split(text, sep)
	var result []string
	for _, str := range arr {
		if str != "" {
			result = append(result, str)
		}
	}
	return result
}

func KeysOfMap[M ~map[K]V, K comparable, V any](m M) []K {
	r := make([]K, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	return r
}

func Check(err error) {
	if err != nil {
		panic(err)
	}
}

func UnionArr[T comparable](arrs ...[]T) []T {
	hit := make(map[T]struct{})
	for _, arr := range arrs {
		for _, text := range arr {
			hit[text] = struct{}{}
		}
	}
	result := make([]T, 0, len(hit))
	for key := range hit {
		result = append(result, key)
	}
	return result
}
