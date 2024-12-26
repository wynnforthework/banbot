package core

const (
	ErrBadConfig = -1*iota - 100
	ErrInvalidPath
	ErrIOReadFail
	ErrIOWriteFail
	ErrDbConnFail
	ErrDbReadFail
	ErrDbExecFail
	ErrDbUniqueViolation
	ErrLiquidation
	ErrLowFunds
	ErrLowSrcAmount
	ErrInvalidCost
	ErrExgNotInit
	ErrCacheErr
	ErrInvalidTF
	ErrInvalidSymbol
	ErrInvalidBars
	ErrInvalidAddr
	ErrRunTime
	ErrMarshalFail
	ErrCompressFail
	ErrDeCompressFail
	ErrTimeout
	ErrEOF

	ErrNetWriteFail
	ErrNetReadFail
	ErrNetUnknown
	ErrNetTimeout
	ErrNetTemporary
	ErrNetConnect
)

var ErrCodeNames = map[int]string{
	ErrBadConfig:         "BadConfig",
	ErrInvalidPath:       "InvalidPath",
	ErrIOReadFail:        "IOReadFail",
	ErrIOWriteFail:       "IOWriteFail",
	ErrDbConnFail:        "DbConnFail",
	ErrDbReadFail:        "DbReadFail",
	ErrDbExecFail:        "DbExecFail",
	ErrDbUniqueViolation: "DbUniqueViolation",
	ErrLiquidation:       "Liquidation",
	ErrLowFunds:          "LowFunds",
	ErrLowSrcAmount:      "LowSrcAmount",
	ErrInvalidCost:       "InvalidCost",
	ErrExgNotInit:        "ExgNotInit",
	ErrCacheErr:          "CacheErr",
	ErrInvalidTF:         "InvalidTF",
	ErrInvalidSymbol:     "InvalidSymbol",
	ErrInvalidBars:       "InvalidBars",
	ErrInvalidAddr:       "InvalidAddr",
	ErrRunTime:           "RunTime",
	ErrMarshalFail:       "MarshalFail",
	ErrCompressFail:      "CompressFail",
	ErrDeCompressFail:    "DeCompressFail",
	ErrTimeout:           "Timeout",
	ErrEOF:               "EOF",
	ErrNetWriteFail:      "NetWriteFail",
	ErrNetReadFail:       "NetReadFail",
	ErrNetUnknown:        "NetUnknown",
	ErrNetTimeout:        "NetTimeout",
	ErrNetTemporary:      "NetTemporary",
	ErrNetConnect:        "NetConnect",
}
