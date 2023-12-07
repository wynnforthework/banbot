package strategy

import (
	"github.com/anyongjin/gobanbot/config"
	"github.com/anyongjin/gobanbot/log"
	"github.com/pkujhd/goloader"
	"go.uber.org/zap"
	"os"
	"path"
	"unsafe"
)

var (
	defPkgName = "main"
	StagyMap   = make(map[string]*TradeStagy) // 已加载的策略缓存
)

func Get(stagyName string) *TradeStagy {
	obj, ok := StagyMap[stagyName]
	if ok {
		return obj
	}
	obj = load(stagyName)
	if obj != nil {
		StagyMap[stagyName] = obj
	}
	return obj
}

func load(stagyName string) *TradeStagy {
	filePath := path.Join(config.GetStagyDir(), stagyName+".o")
	_, err := os.Stat(filePath)
	nameVar := zap.String("name", stagyName)
	if err != nil {
		log.Error("strategy not found", nameVar)
		return nil
	}
	file, err := os.Open(filePath)
	if err != nil {
		log.Error("strategy read fail", nameVar)
		return nil
	}
	linker, err := goloader.ReadObj(file, &defPkgName)
	if err != nil {
		log.Error("strategy load fail, package is `main`?", nameVar)
		return nil
	}
	symPtr := make(map[string]uintptr)
	err = goloader.RegSymbol(symPtr)
	if err != nil {
		log.Error("strategy read symbol fail", nameVar)
		return nil
	}
	module, err := goloader.Load(linker, symPtr)
	if err != nil {
		log.Error("strategy load module fail", nameVar)
		return nil
	}
	main, ok := module.Syms["main.Main"]
	if !ok || main == 0 {
		log.Error("strategy `Main` not found", nameVar)
		return nil
	}
	mainPtr := (uintptr)(unsafe.Pointer(&main))
	stagy := (*TradeStagy)(unsafe.Pointer(&mainPtr))
	module.Unload()
	stagy.Name = stagyName
	return stagy
}
