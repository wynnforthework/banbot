package orm

import (
	"context"
	"errors"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"net"
	"runtime"
	"strings"
	"sync"
)

var (
	pool         *pgxpool.Pool
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
	//poolCfg.BeforeAcquire = func(ctx context.Context, conn *pgx.Conn) bool {
	//	return true
	//}
	//poolCfg.AfterRelease = func(conn *pgx.Conn) bool {
	//  // 此函数不是在调用Release时必定被调用，连接可能直接被Destroy而不释放到池
	//	return true
	//}
	//poolCfg.BeforeClose = func(conn *pgx.Conn) {
	//	log.Info(fmt.Sprintf("close conn: %v", conn))
	//}
	dbPool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return errs.New(core.ErrDbConnFail, err)
	}
	pool = dbPool
	ctx := context.Background()
	row := pool.QueryRow(ctx, "show max_connections;")
	var maxConnections string
	err = row.Scan(&maxConnections)
	if err != nil {
		return NewDbErr(core.ErrDbReadFail, err)
	}
	log.Info("connect db ok", zap.String("url", dbCfg.Url), zap.Int("pool", dbCfg.MaxPoolSize),
		zap.String("DB_MAX_CONN", maxConnections))
	err2 := LoadAllExSymbols()
	if err2 != nil {
		return err2
	}
	sess, conn, err2 := Conn(ctx)
	if err2 != nil {
		return err2
	}
	defer conn.Release()
	return sess.UpdatePendingIns()
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
		return NewDbErr(core.ErrDbExecFail, err)
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
		return NewDbErr(core.ErrDbExecFail, err_)
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
Find unsaved orders from open orders and save them all to the database
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

func LoadMarkets(exchange banexg.BanExchange, reload bool) (banexg.MarketMap, *errs.Error) {
	exInfo := exchange.Info()
	args := make(map[string]interface{})
	if exInfo.ID == "china" && exInfo.MarketType != banexg.MarketSpot {
		items := GetExSymbols(exInfo.ID, exInfo.MarketType)
		symbols := make([]string, 0, len(items))
		for _, it := range items {
			if it.Symbol == "" {
				return nil, errs.NewMsg(errs.CodeRunTime, "symbol empty for sid: %v", it.ID)
			}
			symbols = append(symbols, it.Symbol)
		}
		args[banexg.ParamSymbols] = symbols
	}
	return exchange.LoadMarkets(reload, args)
}

func InitExg(exchange banexg.BanExchange) *errs.Error {
	// LoadMarkets will be called internally
	// 内部会调用LoadMarkets
	err := EnsureExgSymbols(exchange)
	if err != nil {
		return err
	}
	return exchange.LoadLeverageBrackets(false, nil)
}

func (a *AdjInfo) Apply(bars []*banexg.Kline, adj int) []*banexg.Kline {
	if a == nil || a.CumFactor == 1 || a.CumFactor == 0 {
		return bars
	}
	result := make([]*banexg.Kline, 0, len(bars))
	factor := float64(1)
	if adj == core.AdjFront {
		factor = a.CumFactor
	} else if adj == core.AdjBehind {
		factor = 1 / a.CumFactor
	} else {
		return bars
	}
	for _, b := range bars {
		k := b.Clone()
		k.Open *= factor
		k.High *= factor
		k.Low *= factor
		k.Close *= factor
		k.Volume *= factor
		k.Info *= factor
		result = append(result, k)
	}
	return result
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

func NewDbErr(code int, err_ error) *errs.Error {
	var opErr *net.OpError
	if errors.As(err_, &opErr) {
		if strings.Contains(opErr.Err.Error(), "connection reset") {
			return errs.New(core.ErrDbConnFail, err_)
		}
	}
	return errs.New(code, err_)
}
