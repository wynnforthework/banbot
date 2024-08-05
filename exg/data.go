package exg

import (
	"github.com/banbox/banexg"
	"sync"
)

var Default banexg.BanExchange
var exgMap = map[string]banexg.BanExchange{}
var exgMapLock sync.Mutex
