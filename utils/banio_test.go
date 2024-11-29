package utils

import (
	"context"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestBanServer(t *testing.T) {
	core.SetRunMode(core.RunModeLive)
	server := NewBanServer("127.0.0.1:6789", "spider")
	go func() {
		for {
			time.Sleep(time.Millisecond * 300)
			for _, conn := range server.Conns {
				if conn.IsClosed() {
					continue
				}
				err_ := conn.WriteMsg(&IOMsg{Action: "ping", Data: 1})
				if err_ != nil {
					log.Warn("broadcast fail", zap.Error(err_))
				}
			}
		}
	}()
	err := server.RunForever()
	if err != nil {
		panic(err)
	}
}

func TestBanClient(t *testing.T) {
	core.SetRunMode(core.RunModeLive)
	ctx, cancel := context.WithCancel(context.Background())
	core.Ctx = ctx
	core.StopAll = cancel
	log.Setup("debug", "")
	client, err := NewClientIO("127.0.0.1:6789")
	if err != nil {
		panic(err)
	}
	go client.LoopPing(1)
	go client.RunForever()
	err = client.SetVal(&KeyValExpire{
		Key: "vv1",
		Val: "vvvv",
	})
	if err != nil {
		panic(err)
	}
	val, err := client.GetVal("vv1", 5)
	if err != nil {
		panic(err)
	}
	log.Info("get val of vv1", zap.String("val", val))
	lockVal, err := GetNetLock("lk1", 5)
	if err != nil {
		panic(err)
	}
	log.Info("set lock", zap.Int32("val", lockVal))
	val, err = client.GetVal("lock_lk1", 5)
	if err != nil {
		panic(err)
	}
	log.Info("lock real val", zap.String("val", val))
	time.Sleep(time.Second * 3)
	err = DelNetLock("lk1", lockVal)
	if err != nil {
		panic(err)
	}
	val, err = client.GetVal("lock_lk1", 5)
	if err != nil {
		panic(err)
	}
	log.Info("lock val after del", zap.String("val", val))
}
