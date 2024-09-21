package biz

import (
	"fmt"
	"github.com/banbox/banbot/utils"
	"net"
	"net/http"
	_ "net/http/pprof"
	"sync"

	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/exg"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

type FnFeaStream = func(exsList []*orm.ExSymbol, req *SubReq, rsp FeaFeeder_SubFeaturesServer) error

var (
	FeaGenerators = map[string]FnFeaStream{} // SubFeatures 的任务特征生成函数注册
)

func RunDataServer(args *config.CmdArgs) *errs.Error {
	if args.MemProfile {
		go func() {
			log.Info("mem profile serve http at :8080 ...")
			err_ := http.ListenAndServe(":8080", nil)
			if err_ != nil {
				log.Error("run mem profile http fail", zap.Error(err_))
			}
		}()
	}
	core.SetRunMode(core.RunModeOther)
	err := SetupComs(args)
	if err != nil {
		return err
	}
	err = orm.LoadAllExSymbols()
	if err != nil {
		return err
	}
	err = orm.InitExg(exg.Default)
	if err != nil {
		return err
	}
	port := "6789"
	lis, err_ := net.Listen("tcp", ":"+port)
	if err_ != nil {
		return errs.New(errs.CodeNetFail, err_)
	}
	s := grpc.NewServer()
	RegisterFeaFeederServer(s, &DataServer{})
	log.Info(fmt.Sprintf("data server ready, grpc listen at port: %s ...", port))
	err_ = s.Serve(lis)
	if err_ != nil {
		return errs.New(errs.CodeNetFail, err_)
	}
	return nil
}

type DataServer struct {
	*UnimplementedFeaFeederServer
	feaLock sync.Mutex
}

/*
SubFeatures 订阅特征数据流
需要自行注册任务特征数据流生成函数到 FeaGenerators 中，才能正常发起grpc请求
*/
func (s *DataServer) SubFeatures(req *SubReq, rsp FeaFeeder_SubFeaturesServer) error {
	s.feaLock.Lock()
	exchange, err := exg.GetWith(req.Exchange, req.Market, "")
	if err != nil {
		s.feaLock.Unlock()
		return err
	}
	_, err = orm.LoadMarkets(exchange, false)
	if err != nil {
		s.feaLock.Unlock()
		return err
	}
	codes, dups := utils.UniqueItems(req.Codes)
	if len(dups) > 0 {
		log.Info("found duplicate codes", zap.Int("valid", len(codes)), zap.Strings("dups", dups))
	}
	exsList := make([]*orm.ExSymbol, 0, len(codes))
	for _, code := range codes {
		exs, err := orm.GetExSymbol(exchange, code)
		if err != nil {
			s.feaLock.Unlock()
			return err
		}
		exsList = append(exsList, exs)
	}
	s.feaLock.Unlock()
	gen, ok := FeaGenerators[req.Task]
	if !ok {
		return fmt.Errorf("unsupport data task:" + req.Task)
	}
	return gen(exsList, req, rsp)
}
