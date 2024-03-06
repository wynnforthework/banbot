package core

import (
	"github.com/banbox/banexg/errs"
	"github.com/dgraph-io/ristretto"
)

var (
	Cache *ristretto.Cache
)

func Setup() *errs.Error {
	var err_ error
	Cache, err_ = ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,
		MaxCost:     1 << 26,
		BufferItems: 64,
	})
	if err_ != nil {
		return errs.New(ErrRunTime, err_)
	}
	return nil
}

func GetCacheVal[T any](key string, defVal T) T {
	numObj, hasNum := Cache.Get(key)
	if hasNum {
		if numVal, ok := numObj.(T); ok {
			return numVal
		}
	}
	return defVal
}
