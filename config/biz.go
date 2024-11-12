package config

import (
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	utils2 "github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/mitchellh/mapstructure"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func GetDataDir() string {
	if DataDir == "" {
		DataDir = os.Getenv("BanDataDir")
	}
	return DataDir
}

func GetStratDir() string {
	result := os.Getenv("BanStgyDir")
	if result == "" {
		panic(fmt.Errorf("`BanStgyDir` env is required"))
	}
	return result
}

func LoadConfig(args *CmdArgs) *errs.Error {
	if Loaded {
		return nil
	}
	Args = args
	DataDir = args.DataDir
	yamlData = nil
	var paths []string
	if !args.NoDefault {
		dataDir := GetDataDir()
		if dataDir == "" {
			return errs.NewMsg(errs.CodeParamRequired, "data_dir is required")
		}
		tryNames := []string{"config.yml", "config.local.yml"}
		for _, name := range tryNames {
			path := filepath.Join(dataDir, name)
			if _, err := os.Stat(path); err == nil {
				paths = append(paths, path)
			}
		}
	}
	if args.Configs != nil {
		paths = append(paths, args.Configs...)
	}
	var merged = make(map[string]interface{})
	for _, path := range paths {
		log.Info("Using " + path)
		fileData, err := os.ReadFile(ParsePath(path))
		if err != nil {
			return errs.NewFull(core.ErrIOReadFail, err, "Read %s Fail", path)
		}
		var unpak map[string]interface{}
		err = yaml.Unmarshal(fileData, &unpak)
		if err != nil {
			return errs.NewFull(errs.CodeUnmarshalFail, err, "Unmarshal %s Fail", path)
		}
		for key := range noExtends {
			if _, ok := unpak[key]; ok {
				delete(merged, key)
			}
		}
		utils2.DeepCopyMap(merged, unpak)
	}
	err := mapstructure.Decode(merged, &Data)
	if err != nil {
		return errs.NewFull(errs.CodeUnmarshalFail, err, "decode Config Fail")
	}
	return apply(args)
}

func apply(args *CmdArgs) *errs.Error {
	Loaded = true
	NoDB = args.NoDb
	if args.TimeRange != "" {
		Data.TimeRangeRaw = args.TimeRange
	}
	if args.MaxPoolSize > 0 {
		Data.Database.MaxPoolSize = args.MaxPoolSize
	}
	cutLen := len(Data.TimeRangeRaw) / 2
	Data.TimeRange = &TimeTuple{
		btime.ParseTimeMS(Data.TimeRangeRaw[:cutLen]),
		btime.ParseTimeMS(Data.TimeRangeRaw[cutLen+1:]),
	}
	Name = Data.Name
	core.SetRunEnv(Data.Env)
	Leverage = Data.Leverage
	if Data.LimitVolSecs == 0 {
		Data.LimitVolSecs = 10
	}
	LimitVolSecs = Data.LimitVolSecs
	if Data.PutLimitSecs == 0 {
		Data.PutLimitSecs = 120
	}
	PutLimitSecs = Data.PutLimitSecs
	core.ExgName = Data.Exchange.Name
	core.Market = Data.MarketType
	if core.Market == banexg.MarketSpot || core.Market == banexg.MarketMargin {
		Data.ContractType = ""
	} else if Data.ContractType == "" {
		Data.ContractType = banexg.MarketSwap
	}
	core.ContractType = Data.ContractType
	if Data.OdBookTtl == 0 {
		Data.OdBookTtl = 500
	}
	OdBookTtl = Data.OdBookTtl
	StopEnterBars = Data.StopEnterBars
	if Data.ConcurNum == 0 {
		Data.ConcurNum = 2
	}
	core.ConcurNum = Data.ConcurNum
	OrderType = Data.OrderType
	PreFire = Data.PreFire
	if Data.MarginAddRate == 0 {
		Data.MarginAddRate = 0.66
	}
	MarginAddRate = Data.MarginAddRate
	ChargeOnBomb = Data.ChargeOnBomb
	TakeOverStgy = Data.TakeOverStgy
	if args.StakeAmount > 0 {
		Data.StakeAmount = args.StakeAmount
	}
	StakeAmount = Data.StakeAmount
	if args.StakePct > 0 {
		Data.StakePct = args.StakePct
	}
	StakePct = Data.StakePct
	MaxStakeAmt = Data.MaxStakeAmt
	if Data.OpenVolRate == 0 {
		Data.OpenVolRate = 1
	}
	OpenVolRate = Data.OpenVolRate
	if Data.MinOpenRate == 0 {
		Data.MinOpenRate = 0.5
	}
	MinOpenRate = Data.MinOpenRate
	if Data.BTNetCost == 0 {
		Data.BTNetCost = 15
	}
	BTNetCost = Data.BTNetCost
	if Data.MaxOpenOrders == 0 {
		Data.MaxOpenOrders = 30
	}
	MaxOpenOrders = Data.MaxOpenOrders
	WalletAmounts = Data.WalletAmounts
	DrawBalanceOver = Data.DrawBalanceOver
	StakeCurrency = Data.StakeCurrency
	if len(StakeCurrency) == 0 {
		panic("config `stake_currency` cannot be empty")
	}
	StakeCurrencyMap = make(map[string]bool)
	for _, curr := range Data.StakeCurrency {
		StakeCurrencyMap[curr] = true
	}
	FatalStop = make(map[int]float64)
	if len(Data.FatalStop) > 0 {
		for text, rate := range Data.FatalStop {
			mins, err_ := strconv.Atoi(text)
			if err_ != nil || mins < 1 {
				panic("config fatal_stop." + text + " invalid, must be int >= 1")
			}
			FatalStop[mins] = rate
		}
	}
	if Data.FatalStopHours == 0 {
		Data.FatalStopHours = 8
	}
	FatalStopHours = Data.FatalStopHours
	TimeRange = Data.TimeRange
	if len(args.TimeFrames) > 0 {
		Data.RunTimeframes = args.TimeFrames
	}
	RunTimeframes = Data.RunTimeframes
	WatchJobs = Data.WatchJobs
	staticPairs, fixPairs := initPolicies()
	if Data.StrtgPerf == nil {
		Data.StrtgPerf = &StrtgPerfConfig{
			MinOdNum:  5,
			MaxOdNum:  30,
			MinJobNum: 10,
			MidWeight: 0.3,
			BadWeight: 0.15,
		}
	} else {
		Data.StrtgPerf.Validate()
	}
	StrtgPerf = Data.StrtgPerf
	if len(args.Pairs) > 0 {
		Data.Pairs = args.Pairs
	}
	if len(Data.Pairs) > 0 {
		staticPairs = true
		fixPairs = append(fixPairs, Data.Pairs...)
	}
	if staticPairs && len(fixPairs) > 0 {
		Data.Pairs, _ = utils2.UniqueItems(fixPairs)
	}
	Pairs, _ = utils2.UniqueItems(Data.Pairs)
	if Data.PairMgr == nil {
		Data.PairMgr = &PairMgrConfig{}
	}
	PairMgr = Data.PairMgr
	PairFilters = Data.PairFilters
	Exchange = Data.Exchange
	initExgAccs()
	Database = Data.Database
	SpiderAddr = Data.SpiderAddr
	if SpiderAddr == "" {
		SpiderAddr = "127.0.0.1:6789"
	}
	APIServer = Data.APIServer
	RPCChannels = Data.RPCChannels
	Webhook = Data.Webhook
	return nil
}

func initPolicies() (bool, []string) {
	if Data.RunPolicy == nil {
		Data.RunPolicy = make([]*RunPolicyConfig, 0)
	}
	var polPairs []string
	staticPairs := true
	for _, pol := range Data.RunPolicy {
		if pol.Params == nil {
			pol.Params = make(map[string]float64)
		}
		if pol.PairParams == nil {
			pol.PairParams = make(map[string]map[string]float64)
		}
		pol.defs = make(map[string]*core.Param)
		if len(pol.Pairs) > 0 {
			polPairs = append(polPairs, pol.Pairs...)
		} else {
			staticPairs = false
		}
	}
	RunPolicy = Data.RunPolicy
	return staticPairs, polPairs
}

func GetExgConfig() *ExgItemConfig {
	if cfg, ok := Exchange.Items[Exchange.Name]; ok {
		return cfg
	}
	return &ExgItemConfig{}
}

func GetTakeOverTF(pair, defTF string) string {
	if pairMap, ok := core.StgPairTfs[TakeOverStgy]; ok {
		if tf, ok := pairMap[pair]; ok {
			return tf
		}
	}
	return defTF
}

func GetAccLeverage(account string) float64 {
	acc, ok := Accounts[account]
	if ok && acc.Leverage > 0 {
		return acc.Leverage
	} else {
		return Leverage
	}
}

func initExgAccs() {
	if Exchange.Name == "china" {
		btime.LocShow, _ = time.LoadLocation("Asia/Shanghai")
	} else {
		btime.LocShow = btime.UTCLocale
	}
	exgCfg := GetExgConfig()
	var accs map[string]*AccountConfig
	if core.RunEnv != core.RunEnvTest {
		accs = exgCfg.AccountProds
	} else {
		accs = exgCfg.AccountTests
	}
	Accounts = make(map[string]*AccountConfig)
	BakAccounts = make(map[string]*AccountConfig)
	if len(accs) == 0 {
		return
	}
	if core.EnvReal {
		DefAcc = ""
	}
	for name, val := range accs {
		if val.NoTrade {
			BakAccounts[name] = val
		} else if !core.EnvReal {
			// Non-production environment, only enable one account
			// 非生产环境，只启用一个账号
			Accounts[DefAcc] = val
		} else {
			Accounts[name] = val
			if DefAcc == "" {
				// Production environment, the first record is the default account
				// 生产环境，第一个记录为默认账户
				DefAcc = name
			}
		}
	}
}

func (p *StrtgPerfConfig) Validate() {
	if p.MinOdNum < 5 {
		p.MinOdNum = 5
	}
	if p.MaxOdNum < 8 {
		p.MaxOdNum = 8
	}
	if p.MinJobNum < 7 {
		p.MinJobNum = 7
	}
	if p.MidWeight == 0 {
		p.MidWeight = 0.4
	}
	if p.BadWeight == 0 {
		p.BadWeight = 0.15
	}
}

func ParsePath(path string) string {
	if strings.HasPrefix(path, "$") {
		return strings.Replace(path, "$", GetDataDir(), 1)
	}
	return path
}

func GetStakeAmount(accName string) float64 {
	var amount float64
	acc, ok := Accounts[accName]
	// Prioritize using percentage to open orders
	// 优先使用百分比开单
	if ok && acc.StakePctAmt > 0 {
		amount = acc.StakePctAmt
	} else {
		amount = StakeAmount
	}
	// Check if the maximum amount is exceeded
	// 检查是否超出最大金额
	if ok && acc.MaxStakeAmt > 0 && acc.MaxStakeAmt < amount {
		amount = acc.MaxStakeAmt
	} else if MaxStakeAmt > 0 && MaxStakeAmt < amount {
		amount = MaxStakeAmt
	}
	// Multiply by the account multiplier
	// 乘以账户倍率
	if ok && acc.StakeRate > 0 {
		amount *= acc.StakeRate
	}
	return amount
}

func DumpYaml() ([]byte, *errs.Error) {
	if yamlData != nil {
		return yamlData, nil
	}
	itemMap := make(map[string]interface{})
	t := reflect.TypeOf(Data)
	v := reflect.ValueOf(Data)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		val := v.Field(i)
		itemMap[tag] = val.Interface()
	}
	data, err_ := yaml.Marshal(&itemMap)
	if err_ != nil {
		return nil, errs.New(errs.CodeMarshalFail, err_)
	}
	return data, nil
}

func (c *RunPolicyConfig) ID() string {
	if c.Dirt == "" {
		return c.Name
	}
	if c.Dirt == "long" {
		return c.Name + ":l"
	} else if c.Dirt == "short" {
		return c.Name + ":s"
	} else {
		panic(fmt.Sprintf("unknown run_policy dirt: %v", c.Dirt))
	}
}

func (c *RunPolicyConfig) Key() string {
	tfStr := strings.Join(c.RunTimeframes, "|")
	pairStr := strings.Join(c.Pairs, "|")
	return fmt.Sprintf("%s/%s/%s", c.ID(), tfStr, pairStr)
}

func (c *RunPolicyConfig) OdDirt() int {
	dirt := core.OdDirtBoth
	if c.Dirt == "long" {
		dirt = core.OdDirtLong
	} else if c.Dirt == "short" {
		dirt = core.OdDirtShort
	} else if c.Dirt != "" {
		panic(fmt.Sprintf("unknown run_policy dirt: %v", c.Dirt))
	}
	return dirt
}

func (c *RunPolicyConfig) Param(k string, dv float64) float64 {
	if v, ok := c.Params[k]; ok {
		return v
	}
	return dv
}

func (c *RunPolicyConfig) Def(k string, dv float64, p *core.Param) float64 {
	val := c.Param(k, dv)
	if p.Mean == 0 {
		p.Mean = dv
	}
	if p.VType == core.VTypeNorm && p.Rate == 0 {
		p.Rate = 1
	}
	if p.Name == "" {
		p.Name = k
	}
	if c.defs == nil {
		c.defs = make(map[string]*core.Param)
	}
	c.defs[k] = p
	return val
}

func (c *RunPolicyConfig) HyperParams() []*core.Param {
	res := make([]*core.Param, 0, len(c.defs))
	for _, p := range c.defs {
		res = append(res, p)
	}
	return res
}

/*
KeepHyperOnly Only keep the given hyperparameters for optimization and remove other hyperparameters
*/
func (c *RunPolicyConfig) KeepHyperOnly(keys ...string) {
	var res = make(map[string]*core.Param)
	for _, k := range keys {
		if v, ok := c.defs[k]; ok {
			res[k] = v
		}
	}
	c.defs = res
}

func (c *RunPolicyConfig) ToYaml() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("  - name: %s\n", c.Name))
	if c.Dirt != "" {
		b.WriteString(fmt.Sprintf("    dirt: %s\n", c.Dirt))
	}
	if len(c.RunTimeframes) > 0 {
		b.WriteString(fmt.Sprintf("    run_timeframes: [ %s ]\n", strings.Join(c.RunTimeframes, ", ")))
	}
	if len(c.Pairs) > 0 {
		b.WriteString(fmt.Sprintf("    pairs: [ %s ]\n", strings.Join(c.Pairs, ", ")))
	}
	argText, _ := utils2.MapToStr(c.Params)
	if len(c.Pairs) == 1 {
		b.WriteString("    pair_params:\n")
		b.WriteString(fmt.Sprintf("      %s: {%s}\n", c.Pairs[0], argText))
	} else {
		b.WriteString(fmt.Sprintf("    params: {%s}\n", argText))
	}
	return b.String()
}

func (c *RunPolicyConfig) Clone() *RunPolicyConfig {
	res := &RunPolicyConfig{
		Name:          c.Name,
		Filters:       c.Filters,
		RunTimeframes: c.RunTimeframes,
		MaxPair:       c.MaxPair,
		MaxOpen:       c.MaxOpen,
		Dirt:          c.Dirt,
		StrtgPerf:     c.StrtgPerf,
		Pairs:         c.Pairs,
		Params:        make(map[string]float64),
		PairParams:    make(map[string]map[string]float64),
		defs:          make(map[string]*core.Param),
	}
	if len(c.Params) > 0 {
		for k, v := range c.Params {
			res.Params[k] = v
		}
	}
	if len(c.PairParams) > 0 {
		for k, mp := range c.PairParams {
			pairPms := make(map[string]float64)
			for k2, v := range mp {
				pairPms[k2] = v
			}
			res.PairParams[k] = pairPms
		}
	}
	if len(c.defs) > 0 {
		for k, v := range c.defs {
			res.defs[k] = v
		}
	}
	return res
}

func (c *RunPolicyConfig) PairDup(pair string) (*RunPolicyConfig, bool) {
	params, _ := c.PairParams[pair]
	isDiff := true
	if len(params) == 0 {
		params = c.Params
		isDiff = false
	}
	res := c.Clone()
	res.Params = params
	return res, isDiff
}

func LoadPerfs(inDir string) {
	if StrtgPerf == nil || !StrtgPerf.Enable {
		return
	}
	inPath := fmt.Sprintf("%s/strtg_perfs.yml", inDir)
	_, err_ := os.Stat(inPath)
	if err_ != nil {
		return
	}
	data, err_ := os.ReadFile(inPath)
	if err_ != nil {
		log.Error("read strtg_perfs.yml fail", zap.Error(err_))
		return
	}
	var unpak map[string]map[string]interface{}
	err_ = yaml.Unmarshal(data, &unpak)
	if err_ != nil {
		log.Error("unmarshal strtg_perfs fail", zap.Error(err_))
		return
	}
	for strtg, cfg := range unpak {
		sta := &core.PerfSta{}
		err_ = mapstructure.Decode(cfg, &sta)
		if err_ != nil {
			log.Error(fmt.Sprintf("decode %s fail", strtg), zap.Error(err_))
			continue
		}
		core.StratPerfSta[strtg] = sta
		perfVal, ok := cfg["perf"]
		if ok && perfVal != nil {
			var perf = map[string]string{}
			err_ = mapstructure.Decode(perfVal, &perf)
			if err_ != nil {
				log.Error(fmt.Sprintf("decode %s.perf fail", strtg), zap.Error(err_))
				continue
			}
			for pairTf, arrStr := range perf {
				arr := strings.Split(arrStr, "|")
				num, _ := strconv.Atoi(arr[0])
				profit, _ := strconv.ParseFloat(arr[1], 64)
				score, _ := strconv.ParseFloat(arr[2], 64)
				core.JobPerfs[fmt.Sprintf("%s_%s", strtg, pairTf)] = &core.JobPerf{
					Num:       num,
					TotProfit: profit,
					Score:     score,
				}
			}
		}
	}
	log.Info("load strtg_perfs ok", zap.String("path", inPath))
}
