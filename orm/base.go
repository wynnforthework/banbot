package orm

import (
	"context"
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"runtime"
	"strings"
	"sync"
)

var (
	pool         *pgxpool.Pool
	connUsed     int
	connStacks   = make(map[string]string)
	dbLock       sync.Mutex
	accTasks     = make(map[string]*BotTask)
	taskIdAccMap = make(map[int64]string)
)

func Setup() *errs.Error {
	if pool != nil {
		pool.Close()
		pool = nil
	}
	dbCfg := config.Database
	if dbCfg == nil {
		return errs.NewMsg(core.ErrBadConfig, "database config is missing!")
	}
	poolCfg, err_ := pgxpool.ParseConfig(dbCfg.Url)
	if err_ != nil {
		return errs.New(core.ErrBadConfig, err_)
	}
	if dbCfg.MaxPoolSize == 0 {
		dbCfg.MaxPoolSize = max(4, runtime.NumCPU())
	}
	poolCfg.MaxConns = int32(dbCfg.MaxPoolSize)
	poolCfg.BeforeAcquire = func(ctx context.Context, conn *pgx.Conn) bool {
		dbLock.Lock()
		defer dbLock.Unlock()
		if connUsed >= dbCfg.MaxPoolSize {
			var b strings.Builder
			b.WriteString("\n")
			for key, stack := range connStacks {
				b.WriteString(fmt.Sprintf("【Conn】%s\n", key))
				b.WriteString(stack)
				b.WriteString("\n")
			}
			log.Error("all db conn are in used", zap.Int("num", connUsed), zap.String("more", b.String()))
		}
		connStacks[fmt.Sprintf("%p", conn)] = errs.CallStack(2, 30)
		connUsed += 1
		return true
	}
	poolCfg.AfterRelease = func(conn *pgx.Conn) bool {
		dbLock.Lock()
		defer dbLock.Unlock()
		delete(connStacks, fmt.Sprintf("%p", conn))
		connUsed -= 1
		return true
	}
	//poolCfg.BeforeClose = func(conn *pgx.Conn) {
	//	log.Info(fmt.Sprintf("close conn: %v", conn))
	//}
	dbPool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return errs.New(core.ErrDbConnFail, err)
	}
	pool = dbPool
	row := pool.QueryRow(context.Background(), "show timezone;")
	var tz string
	err = row.Scan(&tz)
	if err != nil {
		return errs.New(core.ErrDbReadFail, err)
	}
	log.Info("connect db ok", zap.String("url", dbCfg.Url), zap.Int("pool", dbCfg.MaxPoolSize))
	return nil
}

func Conn(ctx context.Context) (*Queries, *pgxpool.Conn, *errs.Error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, errs.New(core.ErrDbConnFail, err)
	}
	return New(conn), conn, nil
}

type Tx struct {
	tx     pgx.Tx
	closed bool
}

func (t *Tx) Close(ctx context.Context, commit bool) *errs.Error {
	if t.closed {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var err error
	if commit {
		err = t.tx.Commit(ctx)
	} else {
		err = t.tx.Rollback(ctx)
	}
	t.closed = true
	if err != nil {
		return errs.New(core.ErrDbExecFail, err)
	}
	return nil
}

func (q *Queries) NewTx(ctx context.Context) (*Tx, *Queries, *errs.Error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, nil, errs.New(core.ErrDbConnFail, err)
	}
	nq := q.WithTx(tx)
	return &Tx{tx: tx}, nq, nil
}

func (q *Queries) Exec(sql string, args ...interface{}) *errs.Error {
	_, err_ := q.db.Exec(context.Background(), sql, args...)
	if err_ != nil {
		return errs.New(core.ErrDbExecFail, err_)
	}
	return nil
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

func GetTriggerODs(account string) (map[string][]*InOutOrder, *sync.Mutex) {
	if !core.EnvReal {
		account = config.DefAcc
	}
	val, ok := accTriggerODs[account]
	lock, _ := lockTriggerMap[account]
	if !ok {
		val = make(map[string][]*InOutOrder)
		accTriggerODs[account] = val
		lock = &sync.Mutex{}
		lockTriggerMap[account] = lock
	}
	return val, lock
}

func AddTriggerOd(account string, od *InOutOrder) {
	triggerOds, lock := GetTriggerODs(account)
	lock.Lock()
	ods, _ := triggerOds[od.Symbol]
	triggerOds[od.Symbol] = append(ods, od)
	lock.Unlock()
}

/*
OpenNum
返回符合指定状态的尚未平仓订单的数量
*/
func OpenNum(account string, status int16) int {
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
从打开的订单中查找未保存的订单，全部保存到数据库
*/
func SaveDirtyODs(account string) *errs.Error {
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
	sess, conn, err := Conn(nil)
	if err != nil {
		return err
	}
	defer conn.Release()
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
