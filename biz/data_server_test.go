package biz

import (
	"context"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	utils2 "github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"testing"
	"time"
)

func TestDataServer(t *testing.T) {
	const maxMsgSize = 100 * 1024 * 1024
	addr := "127.0.0.1:6789"
	creds := grpc.WithTransportCredentials(insecure.NewCredentials())
	conn, err_ := grpc.NewClient(addr, creds, grpc.WithDefaultCallOptions(
		grpc.MaxCallSendMsgSize(maxMsgSize),
		grpc.MaxCallRecvMsgSize(maxMsgSize),
	))
	if err_ != nil {
		panic(err_)
	}
	client := NewFeaFeederClient(conn)
	ctx := context.Background()
	tfMSecs := int64(utils2.TFToSecs("1M") * 1000)
	now := time.Now()
	endMS := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, btime.UTCLocale).UnixMilli()
	res, err_ := client.SubFeatures(ctx, &SubReq{
		Exchange: "binance",
		Market:   banexg.MarketLinear,
		Codes:    []string{"BTC/USDT:USDT", "ETH/USDT:USDT", "ETC/USDT:USDT"},
		Start:    endMS - tfMSecs/4,
		End:      endMS, // first day of month
		Task:     "aifea2",
		Sample:   10,
	})
	if err_ != nil {
		panic(err_)
	}
	for {
		data, err_ := res.Recv()
		if err_ != nil {
			if err_ == io.EOF {
				break
			}
			panic(err_)
		}
		barSecs := data.Mats["bar"].Data[0]
		barDate := btime.ToDateStr(int64(barSecs*1000), core.DefaultDateFmt)
		log.Info("receive", zap.Strings("code", data.Codes), zap.Int("keys", len(data.Mats)),
			zap.String("date", barDate))
	}
}
