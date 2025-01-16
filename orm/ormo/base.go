package ormo

import (
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"sync"
)

var (
	accTasks     = make(map[string]*BotTask)
	taskIdAccMap = make(map[int64]string)
)

func Conn(path string, write bool) (*Queries, *errs.Error) {
	db, err := orm.DbLite(orm.DbTrades, path, write)
	if err != nil {
		return nil, err
	}
	return New(db), nil
}

func GetTaskID(account string) int64 {
	task := GetTask(account)
	if task != nil {
		return task.ID
	}
	return 0
}

func GetTask(account string) *BotTask {
	if !core.EnvReal {
		account = config.DefAcc
	}
	if task, ok := accTasks[account]; ok {
		return task
	}
	return nil
}

func GetTaskAcc(id int64) string {
	if acc, ok := taskIdAccMap[id]; ok {
		return acc
	}
	return ""
}

func GetOpenODs(account string) (map[int64]*InOutOrder, *sync.Mutex) {
	if !core.EnvReal {
		account = config.DefAcc
	}
	val, ok := accOpenODs[account]
	lock, _ := lockOpenMap[account]
	if !ok {
		val = make(map[int64]*InOutOrder)
		accOpenODs[account] = val
		lock = &sync.Mutex{}
		lockOpenMap[account] = lock
	}
	return val, lock
}

func GetTriggerODs(account string) (map[string]map[int64]*InOutOrder, *sync.Mutex) {
	if !core.EnvReal {
		account = config.DefAcc
	}
	val, ok := accTriggerODs[account]
	lock, _ := lockTriggerMap[account]
	if !ok {
		val = make(map[string]map[int64]*InOutOrder)
		accTriggerODs[account] = val
		lock = &sync.Mutex{}
		lockTriggerMap[account] = lock
	}
	return val, lock
}

func AddTriggerOd(account string, od *InOutOrder) {
	triggerOds, lock := GetTriggerODs(account)
	lock.Lock()
	ods, ok := triggerOds[od.Symbol]
	if !ok {
		ods = make(map[int64]*InOutOrder)
		triggerOds[od.Symbol] = ods
	}
	ods[od.ID] = od
	lock.Unlock()
}

/*
OpenNum
Returns the number of open orders that match the specified status
返回符合指定状态的尚未平仓订单的数量
*/
func OpenNum(account string, status int64) int {
	openNum := 0
	openOds, lock := GetOpenODs(account)
	lock.Lock()
	for _, od := range openOds {
		if od.Status >= status {
			openNum += 1
		}
	}
	lock.Unlock()
	return openNum
}

/*
SaveDirtyODs
Find unsaved orders from open orders and save them all to the database
从打开的订单中查找未保存的订单，全部保存到数据库
*/
func SaveDirtyODs(path string, account string) *errs.Error {
	var dirtyOds []*InOutOrder
	for accKey, ods := range accOpenODs {
		if account != "" && accKey != account {
			continue
		}
		lock, _ := lockOpenMap[accKey]
		lock.Lock()
		for key, od := range ods {
			if od.IsDirty() {
				dirtyOds = append(dirtyOds, od)
			}
			if od.Status >= InOutStatusFullExit {
				delete(ods, key)
			}
		}
		lock.Unlock()
	}
	if len(dirtyOds) == 0 {
		return nil
	}
	sess, err := Conn(path, true)
	if err != nil {
		return err
	}
	var odErr *errs.Error
	for _, od := range dirtyOds {
		err = od.Save(sess)
		if err != nil {
			odErr = err
			log.Error("save unMatch od fail", zap.String("key", od.Key()), zap.Error(err))
		}
	}
	return odErr
}

func ResetVars() {
	accOpenODs = make(map[string]map[int64]*InOutOrder)
	accTriggerODs = make(map[string]map[string]map[int64]*InOutOrder)
	lockOpenMap = make(map[string]*sync.Mutex)
	lockTriggerMap = make(map[string]*sync.Mutex)
	lockOds = make(map[string]*sync.Mutex)
	accTasks = make(map[string]*BotTask)
	taskIdAccMap = make(map[int64]string)
}

// VarsBackup 存储 ormo 包相关全局变量的备份
type VarsBackup struct {
	AccOpenODs    map[string]map[int64]*InOutOrder
	AccTriggerODs map[string]map[string]map[int64]*InOutOrder
	AccTasks      map[string]*BotTask
	TaskIdAccMap  map[int64]string
}

// BackupVars 备份所有全局变量
func BackupVars() *VarsBackup {
	return &VarsBackup{
		AccOpenODs:    accOpenODs,
		AccTriggerODs: accTriggerODs,
		AccTasks:      accTasks,
		TaskIdAccMap:  taskIdAccMap,
	}
}

// RestoreVars 从备份中恢复所有全局变量
func RestoreVars(backup *VarsBackup) {
	if backup == nil {
		return
	}
	accOpenODs = backup.AccOpenODs
	accTriggerODs = backup.AccTriggerODs
	accTasks = backup.AccTasks
	taskIdAccMap = backup.TaskIdAccMap
}
