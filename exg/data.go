package exg

import (
	"sync"

	"github.com/banbox/banexg"
)

var Default banexg.BanExchange
var exgMap = map[string]banexg.BanExchange{}
var exgMapLock sync.Mutex
var AllowExgIds = map[string]bool{
	"binance": true,
	"bybit":   true,
	"china":   true,
}
