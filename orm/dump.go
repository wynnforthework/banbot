package orm

import (
	"encoding/gob"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/log"
	"github.com/sasha-s/go-deadlock"
	"go.uber.org/zap"
	"os"
)

type DumpRow struct {
	Time int64
	Type string
	Key  string
	Val  interface{}
}

var (
	dumpRows    []*DumpRow
	dumpLock    deadlock.Mutex
	dumpEncoder *gob.Encoder
	dumpFile    *os.File
)

const (
	DumpKline     = "kline"
	DumpStartUp   = "startup"
	DumpApiOrder  = "api_order"
	DumpWsMyTrade = "ws_my_trade"
)

func SetDump(file *os.File) {
	gob.Register(banexg.Kline{})
	gob.Register(banexg.MyTrade{})
	gob.Register(exg.PutOrderRes{})
	dumpLock.Lock()
	dumpFile = file
	dumpEncoder = gob.NewEncoder(file)
	dumpLock.Unlock()
	AddDumpRow(DumpStartUp, "", nil)
}

func AddDumpRow(src, key string, val interface{}) {
	if dumpEncoder == nil {
		return
	}
	dumpLock.Lock()
	dumpRows = append(dumpRows, &DumpRow{
		Time: btime.UTCStamp(),
		Type: src,
		Key:  key,
		Val:  val,
	})
	dumpLock.Unlock()
}

func FlushDumps() {
	if dumpEncoder == nil {
		return
	}
	dumpLock.Lock()
	if len(dumpRows) > 0 {
		err := dumpEncoder.Encode(dumpRows)
		if err != nil {
			log.Error("dump rows fail", zap.Error(err))
		} else {
			err = dumpFile.Sync()
			if err != nil {
				log.Error("flush dump rows fail", zap.Error(err))
			} else {
				dumpRows = nil
			}
		}
	}
	dumpLock.Unlock()
}

func CloseDump() {
	FlushDumps()
	if dumpFile == nil {
		return
	}
	dumpLock.Lock()
	err := dumpFile.Sync()
	if err != nil {
		log.Error("flush dump rows fail", zap.Error(err))
	}
	err = dumpFile.Close()
	if err != nil {
		log.Error("close dump file fail", zap.Error(err))
	}
	dumpFile = nil
	dumpEncoder = nil
	dumpLock.Unlock()
}
