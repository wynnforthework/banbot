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

func ValsOfMap[M ~map[K]V, K comparable, V any](m M) []V {
	r := make([]V, 0, len(m))
	for _, v := range m {
		r = append(r, v)
	}
	return r
}

func CutMap[M ~map[K]V, K comparable, V any](m M, keys ...K) M {
	r := make(M)
	for _, k := range keys {
		if v, ok := m[k]; ok {
			r[k] = v
		}
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

func MapArr[T1, T2 any](arr []T1, doMap func(T1) T2) []T2 {
	var res = make([]T2, len(arr))
	for i, item := range arr {
		res[i] = doMap(item)
	}
	return res
}
