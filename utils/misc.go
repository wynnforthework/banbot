package utils

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
)

var (
	regHolds, _  = regexp.Compile("[{]([^}]+)[}]")
	dockerStatus = 0
)

/*
SplitSolid
String segmentation, ignore empty strings in the return result
字符串分割，忽略返回结果中的空字符串
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
	result := make([]T, 0, len(hit))
	for _, arr := range arrs {
		for _, text := range arr {
			if _, ok := hit[text]; !ok {
				result = append(result, text)
				hit[text] = struct{}{}
			}
		}
	}
	return result
}

func ReverseArr[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func ConvertArr[T1, T2 any](arr []T1, doMap func(T1) T2) []T2 {
	var res = make([]T2, len(arr))
	for i, item := range arr {
		res[i] = doMap(item)
	}
	return res
}

func ArrToMap[T1 comparable, T2 any](arr []T2, doMap func(T2) T1) map[T1][]T2 {
	res := make(map[T1][]T2)
	for _, v := range arr {
		key := doMap(v)
		if old, ok := res[key]; ok {
			res[key] = append(old, v)
		} else {
			res[key] = []T2{v}
		}
	}
	return res
}

func RemoveFromArr[T comparable](arr []T, it T, num int) []T {
	res := make([]T, 0, len(arr))
	for _, v := range arr {
		if v == it && (num < 0 || num > 0) {
			num -= 1
			continue
		}
		res = append(res, v)
	}
	return res
}

func FormatWithMap(text string, args map[string]interface{}) string {
	var b strings.Builder
	matches := regHolds.FindAllStringSubmatchIndex(text, -1)
	var lastEnd int
	for _, mat := range matches {
		start, end := mat[2], mat[3]
		b.WriteString(text[lastEnd : start-1])
		holdText := text[start:end]
		parts := strings.Split(holdText, ":")
		valFmt := "%v"
		if len(parts) > 1 && len(parts[1]) > 0 {
			valFmt = "%" + parts[1]
		}
		if val, ok := args[parts[0]]; ok {
			b.WriteString(fmt.Sprintf(valFmt, val))
		}
		lastEnd = end + 1
	}
	b.WriteString(text[lastEnd:])
	return b.String()
}

func PrintErr(e error) string {
	if e == nil {
		return ""
	}
	var pgErr *pgconn.PgError
	if errors.As(e, &pgErr) {
		return fmt.Sprintf("(SqlState %v) %s: %s, where=%s from %s.%s", pgErr.Code, pgErr.Message,
			pgErr.Detail, pgErr.Where, pgErr.SchemaName, pgErr.TableName)
	}
	return e.Error()
}

func DeepCopyMap(dst, src map[string]interface{}) {
	if src == nil {
		return
	}
	for k, v := range src {
		if vSrcMap, ok := v.(map[string]interface{}); ok {
			if vDstMap, ok := dst[k].(map[string]interface{}); ok {
				DeepCopyMap(vDstMap, vSrcMap)
				continue
			}
		}
		dst[k] = v
	}
}

func ParallelRun[T any](items []T, concurNum int, handle func(int, T) *errs.Error) *errs.Error {
	var retErr *errs.Error
	guard := make(chan struct{}, concurNum)
	var wg sync.WaitGroup
	for i_, item_ := range items {
		// If the concurrency limit is reached, it will block and wait here
		// 如果达到并发限制，这里会阻塞等待
		guard <- struct{}{}
		if retErr != nil {
			// 出错，终止返回
			break
		}
		wg.Add(1)
		go func(i int, item T) {
			defer func() {
				// Complete a task and pop up a pop-up from chan
				// 完成一个任务，从chan弹出一个
				<-guard
				wg.Done()
			}()
			err := handle(i, item)
			if err != nil {
				retErr = err
			}
		}(i_, item_)
	}
	wg.Wait()
	return retErr
}

func ReadInput(tips []string) (string, error) {
	for _, l := range tips {
		fmt.Println(l)
	}
	var input string
	_, err_ := fmt.Scanln(&input)
	if err_ != nil {
		return "", err_
	}
	return input, nil
}

func ReadConfirm(tips []string, ok, fail string, exitAny bool) bool {
	input, err_ := ReadInput(tips)
	if err_ != nil {
		log.Warn("read confirm fail", zap.Error(err_))
		return false
	}
	if input == ok {
		return true
	} else if input == fail {
		return false
	} else if exitAny {
		return false
	}
	tip := fmt.Sprintf("unknown, input %s/%s", ok, fail)
	for {
		input, err_ = ReadInput([]string{tip})
		if err_ != nil {
			log.Warn("read confirm fail", zap.Error(err_))
			return false
		}
		if input == ok {
			return true
		} else if input == fail {
			return false
		}
	}
}

func MD5(data []byte) string {
	hash := md5.New()
	hash.Write(data)
	hashInBytes := hash.Sum(nil)

	return hex.EncodeToString(hashInBytes)
}

func IsDocker() bool {
	if dockerStatus != 0 {
		return dockerStatus == 1
	}
	dockerStatus = -1
	file, err := os.Open("/proc/1/cgroup")
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "docker") {
			dockerStatus = 1
			return true
		}
	}

	return false
}
