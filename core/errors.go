package core

const (
	ErrBadConfig = -1*iota - 100
	ErrInvalidPath
	ErrIOReadFail
	ErrIOWriteFail
	ErrDbConnFail
	ErrDbReadFail
	ErrDbExecFail
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
