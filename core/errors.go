package core

const (
	ErrBadConfig = -1*iota - 100
	ErrInvalidPath
	ErrIOReadFail
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
	ErrInvalidAddr
	ErrRunTime
	ErrMarshalFail
	ErrCompressFail
	ErrDeCompressFail
	ErrTimeout

	ErrNetWriteFail
	ErrNetReadFail

	ErrNetUnknown
	ErrNetTimeout
	ErrNetTemporary
	ErrNetConnect
)
