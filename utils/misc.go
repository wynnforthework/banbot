package utils

import (
	"fmt"
	"math"
	"strings"
)

const thresFloat64Eq = 1e-9

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

/*
NumSign 获取数字的方向；1，-1或0
*/
func NumSign(obj interface{}) int {
	if val, ok := obj.(int); ok {
		if val > 0 {
			return 1
		} else if val < 0 {
			return -1
		} else {
			return 0
		}
	} else if val, ok := obj.(float32); ok {
		if val > 0 {
			return 1
		} else if val < 0 {
			return -1
		} else {
			return 0
		}
	} else if val, ok := obj.(float64); ok {
		if val > 0 {
			return 1
		} else if val < 0 {
			return -1
		} else {
			return 0
		}
	} else {
		panic(fmt.Errorf("invalid type for NumSign: %t", obj))
	}
}

/*
EqualNearly 判断两个float是否近似相等，解决浮点精读导致不等
*/
func EqualNearly(a, b float64) bool {
	return EqualIn(a, b, thresFloat64Eq)
}

/*
EqualIn 判断两个float是否在一定范围内近似相等
*/
func EqualIn(a, b, thres float64) bool {
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	return math.Abs(a-b) <= thres
}
