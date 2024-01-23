package data

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"testing"
)

func TestWatchOhlcv(t *testing.T) {
	client, err := NewKlineWatcher("127.0.0.1:6789")
	if err != nil {
		panic(err)
	}
	err = client.WatchJobs("binance", banexg.MarketLinear, "ohlcv", WatchJob{
		Symbol:    "BTC/USDT:USDT",
		TimeFrame: "1m",
	})
	if err != nil {
		panic(err)
	}
	client.RunForever()
}

func initApp() *errs.Error {
	var args config.CmdArgs
	args.Init()
	err := config.LoadConfig(&args)
	if err != nil {
		return err
	}
	log.Setup(config.Args.Debug, config.Args.Logfile)
	err = exg.Setup()
	if err != nil {
		return err
	}
	return orm.Setup()
}

func TestSaveKlines(t *testing.T) {
	err := initApp()
	if err != nil {
		panic(err)
	}
	sess, conn, err := orm.Conn(nil)
	if err != nil {
		panic(err)
	}
	defer conn.Release()
	arr := []*banexg.Kline{
		{1705984440000, 40131.700000, 40138.900000, 40121.100000, 40127.900000, 58.255000},
		{1705984500000, 40128.300000, 40132.000000, 40122.900000, 40123.000000, 29.978000},
		{1705984560000, 40123.000000, 40125.000000, 40121.000000, 40125.000000, 30.313000},
		{1705984620000, 40124.900000, 40133.000000, 40123.000000, 40128.000000, 33.516000},
		{1705984680000, 40128.000000, 40132.500000, 40123.700000, 40123.700000, 32.557000},
		{1705984740000, 40123.800000, 40123.800000, 40119.000000, 40119.000000, 40.124000},
		{1705984800000, 40119.000000, 40119.100000, 40096.300000, 40096.300000, 66.924000},
		{1705984860000, 40096.300000, 40118.800000, 40095.200000, 40118.700000, 53.286000},
		{1705984920000, 40118.800000, 40118.800000, 40114.000000, 40114.000000, 28.049000},
		{1705984980000, 40114.000000, 40123.800000, 40114.000000, 40123.800000, 62.342000},
		{1705985040000, 40123.700000, 40134.100000, 40123.700000, 40134.000000, 33.549000},
		{1705985100000, 40134.100000, 40134.100000, 40122.000000, 40122.000000, 29.757000},
		{1705985160000, 40122.000000, 40122.100000, 40097.400000, 40097.400000, 67.174000},
		{1705985220000, 40097.400000, 40097.500000, 40086.900000, 40095.600000, 64.617000},
		{1705985280000, 40095.700000, 40095.700000, 40078.900000, 40078.900000, 45.691000},
		{1705985340000, 40078.900000, 40097.300000, 40078.900000, 40097.200000, 82.187000},
	}
	sid := int32(-1)
	timeFrame := "1m"
	err = sess.Exec(fmt.Sprintf(`
delete from kline_1m where sid=%v;
delete from kline_5m where sid=%v;
delete from kline_15m where sid=%v;
delete from kline_1h where sid=%v;
delete from kline_1d where sid=%v;
delete from kline_un where sid=%v;
delete from kinfo where sid=%v;
delete from khole where sid=%v;`, sid, sid, sid, sid, sid, sid, sid, sid))
	if err != nil {
		panic(err)
	}
	core.RunMode = core.RunModeBackTest
	tfMSecs := int64(utils.TFToSecs(timeFrame) * 1000)
	for _, bar := range arr {
		btime.CurTimeMS = bar.Time + tfMSecs
		_, err = sess.InsertKLinesAuto(timeFrame, sid, []*banexg.Kline{bar})
	}
}
