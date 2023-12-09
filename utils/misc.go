package utils

import (
	"strings"
)

func DeepCopy(src, dst map[interface{}]interface{}) {
	for k, v := range src {
		if v, ok := v.(map[interface{}]interface{}); ok {
			if bv, ok := dst[k]; ok {
				if bv, ok := bv.(map[interface{}]interface{}); ok {
					DeepCopy(v, bv)
					continue
				}
			}
		}
		dst[k] = v
	}
}

/*
SplitSolid 字符串分割，忽略返回结果中的空字符串
*/
func SplitSolid(text string, sep string) []string {
	arr := strings.Split(text, sep)
	result := []string{}
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
