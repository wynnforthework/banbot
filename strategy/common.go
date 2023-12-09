package strategy

import (
	"fmt"
	"github.com/anyongjin/banbot/config"
	"github.com/anyongjin/banbot/log"
	"github.com/anyongjin/banbot/utils"
	ta "github.com/anyongjin/banta"
	"github.com/pkujhd/goloader"
	"go.uber.org/zap"
	"os"
	"path"
	"strings"
	"unsafe"
)

var (
	stagyMap = make(map[string]*TradeStagy) // 已加载的策略缓存
)

func Get(stagyName string) *TradeStagy {
	obj, ok := stagyMap[stagyName]
	if ok {
		return obj
	}
	obj = load(stagyName)
	if obj != nil {
		stagyMap[stagyName] = obj
	}
	return obj
}

func load(stagyName string) *TradeStagy {
	filePath := path.Join(config.GetStagyDir(), stagyName+".o")
	_, err := os.Stat(filePath)
	nameVar := zap.String("name", stagyName)
	if err != nil {
		log.Error("strategy not found", zap.String("path", filePath), zap.Error(err))
		return nil
	}
	file, err := os.Open(filePath)
	if err != nil {
		log.Error("strategy read fail", nameVar, zap.Error(err))
		return nil
	}
	linker, err := goloader.ReadObj(file, &stagyName)
	if err != nil {
		log.Error("strategy load fail, package is `main`?", nameVar, zap.Error(err))
		return nil
	}
	symPtr := make(map[string]uintptr)
	err = goloader.RegSymbol(symPtr)
	if err != nil {
		log.Error("strategy read symbol fail", nameVar, zap.Error(err))
		return nil
	}
	regLoaderTypes(symPtr)
	module, err := goloader.Load(linker, symPtr)
	if err != nil {
		log.Error("strategy load module fail", nameVar, zap.Error(err))
		return nil
	}
	main, ok := module.Syms[fmt.Sprintf("%s.Main", stagyName)]
	if !ok || main == 0 {
		keys := zap.String("keys", strings.Join(utils.KeysOfMap(module.Syms), ","))
		log.Error("strategy `Main` not found", nameVar, zap.Error(err), keys)
		return nil
	}
	mainPtr := (uintptr)(unsafe.Pointer(&main))
	runFunc := *(*func() *TradeStagy)(unsafe.Pointer(&mainPtr))
	stagy := runFunc()
	stagy.Name = stagyName
	module.Unload()
	return stagy
}

func regLoaderTypes(symPtr map[string]uintptr) {
	goloader.RegTypes(symPtr, ta.ATR, ta.Highest, ta.Lowest)
	job := &StagyJob{}
	goloader.RegTypes(symPtr, job.OpenOrder)
}
