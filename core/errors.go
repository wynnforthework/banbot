package core

const (
	ErrRunTime        = -100
	ErrTimeout        = -101
	ErrCacheErr       = -102
	ErrMarshalFail    = -103
	ErrCompressFail   = -104
	ErrDeCompressFail = -105

	ErrBadConfig     = -110
	ErrInvalidPath   = -111
	ErrInvalidTF     = -112
	ErrInvalidSymbol = -113
	ErrInvalidBars   = -114
	ErrInvalidAddr   = -115
	ErrInvalidCost   = -116

	ErrDbConnFail        = -120
	ErrDbReadFail        = -121
	ErrDbExecFail        = -122
	ErrDbUniqueViolation = -123

	ErrLiquidation  = -130
	ErrLowFunds     = -131
	ErrLowSrcAmount = -132
	ErrExgNotInit   = -133

	ErrNetWriteFail = -140
	ErrNetReadFail  = -141
	ErrNetUnknown   = -142
	ErrNetTimeout   = -143
	ErrNetTemporary = -144
	ErrNetConnect   = -145

	ErrIOReadFail  = -150
	ErrIOWriteFail = -151
	ErrEOF         = -152
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
