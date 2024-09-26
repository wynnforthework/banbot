package core

import (
	"fmt"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/dgraph-io/ristretto"
	"github.com/mitchellh/mapstructure"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"math"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
)

var (
	Cache *ristretto.Cache
)

func Setup() *errs.Error {
	var err_ error
	Cache, err_ = ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,
		MaxCost:     1 << 26,
		BufferItems: 64,
	})
	if err_ != nil {
		return errs.New(ErrRunTime, err_)
	}
	return nil
}

func GetCacheVal[T any](key string, defVal T) T {
	numObj, hasNum := Cache.Get(key)
	if hasNum {
		if numVal, ok := numObj.(T); ok {
			return numVal
		}
	}
	return defVal
}

func SnapMem(name string) {
	if MemOut == nil {
		log.Warn("core.MemOut is nil, skip memory snapshot")
		return
	}
	err := pprof.Lookup(name).WriteTo(MemOut, 0)
	if err != nil {
		log.Warn("save mem snapshot fail", zap.Error(err))
	}
}

func RunExitCalls() {
	for _, method := range ExitCalls {
		method()
	}
	ExitCalls = nil
}

func KeyStagyPairTf(stagy, pair, tf string) string {
	var b strings.Builder
	b.Grow(len(pair) + len(tf) + len(stagy) + 2)
	b.WriteString(stagy)
	b.WriteString("_")
	b.WriteString(pair)
	b.WriteString("_")
	b.WriteString(tf)
	return b.String()
}

func (p *JobPerf) GetAmount(amount float64) float64 {
	if p.Score == PrefMinRate {
		// 按最小金额开单
		return MinStakeAmount
	}
	return amount * p.Score
}

func GetPerfSta(stagy string) *PerfSta {
	p, ok := StagyPerfSta[stagy]
	if !ok || p == nil {
		p = &PerfSta{}
		StagyPerfSta[stagy] = p
	}
	return p
}

func (p *PerfSta) FindGID(val float64) int {
	if p.Splits == nil {
		panic("PerfSta.Splits is empty, FindGID fail")
	}
	for i, gp := range p.Splits {
		if val < gp {
			return i
		}
	}
	return len(p.Splits)
}

func (p *PerfSta) Log2(profit float64) float64 {
	logPft := math.Log2(math.Abs(profit)*p.Delta + 1)
	if profit < 0 {
		logPft = -logPft
	}
	return logPft
}

func DumpPerfs(outDir string) {
	perfs := make(map[string]map[string]string)
	for key, pf := range JobPerfs {
		parts := strings.Split(key, "_")
		data, ok := perfs[parts[0]]
		if !ok {
			data = make(map[string]string)
			perfs[parts[0]] = data
		}
		cacheKey := strings.Join(parts[1:], "_")
		data[cacheKey] = fmt.Sprintf("%v|%.5f|%.5f", pf.Num, pf.TotProfit, pf.Score)
	}
	res := make(map[string]interface{})
	for name, sta := range StagyPerfSta {
		perf, _ := perfs[name]
		res[name] = map[string]interface{}{
			"od_num":     sta.OdNum,
			"last_gp_at": sta.LastGpAt,
			"splits":     sta.Splits,
			"delta":      sta.Delta,
			"perf":       perf,
		}
	}
	data, err_ := yaml.Marshal(res)
	if err_ != nil {
		log.Error("marshal strtg_perfs fail", zap.Error(err_))
		return
	}
	outName := fmt.Sprintf("%s/strtg_perfs.yml", outDir)
	err_ = os.WriteFile(outName, data, 0644)
	if err_ != nil {
		log.Error("save strtg_perfs fail", zap.Error(err_))
		return
	}
	log.Info("dump strtg_perfs ok", zap.String("path", outName))
}

func LoadPerfs(inDir string) {
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
		sta := &PerfSta{}
		err_ = mapstructure.Decode(cfg, &sta)
		if err_ != nil {
			log.Error(fmt.Sprintf("decode %s fail", strtg), zap.Error(err_))
			continue
		}
		StagyPerfSta[strtg] = sta
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
				JobPerfs[fmt.Sprintf("%s_%s", strtg, pairTf)] = &JobPerf{
					Num:       num,
					TotProfit: profit,
					Score:     score,
				}
			}
		}
	}
	log.Info("load strtg_perfs ok", zap.String("path", inPath))
}

/*
IsFiat 是否是法币
*/
func IsFiat(code string) bool {
	return strings.Contains(code, "USD") || strings.Contains(code, "CNY")
}

func PNorm(min, max float64) *Param {
	return &Param{
		VType: VTypeNorm,
		Min:   min,
		Max:   max,
	}
}

func PNormF(min, max, mean, rate float64) *Param {
	return &Param{
		VType: VTypeNorm,
		Min:   min,
		Max:   max,
		Mean:  mean,
		Rate:  rate,
	}
}

func PUniform(min, max float64) *Param {
	return &Param{
		VType: VTypeUniform,
		Min:   min,
		Max:   max,
	}
}

/*
OptSpace 返回一个均匀分布的区间，用于超参数搜索
*/
func (p *Param) OptSpace() (float64, float64) {
	if p.VType == VTypeNorm {
		return p.toNormXSpace()
	} else {
		return p.Min, p.Max
	}
}

/*
ToRegular 将均匀分布映射为正态分布的超参数值
*/
func (p *Param) ToRegular(x float64) (float64, bool) {
	if p.VType == VTypeNorm {
		scale := max(p.Mean-p.Min, p.Max-p.Mean)
		normVal := p.norm(x) / p.getEdgeY()
		x = normVal*scale + p.Mean
	}
	if x < p.Min || x > p.Max {
		return min(p.Max, max(x, p.Min)), false
	}
	return x, true
}

func (p *Param) getEdgeY() float64 {
	if p.edgeY == 0 {
		p.edgeY = p.norm(0.5)
	}
	return p.edgeY
}

/*
已知当前y值域，返回反正态分布的x对应值域
*/
func (p *Param) toNormXSpace() (float64, float64) {
	// 使用pow(x, 3) + x/(20*rate) 来拟合符合正态分布的值域
	// x : [-0.5, 0.5]
	// y : [-0.15, 0.15]当rate=1
	neg, pos := p.Mean-p.Min, p.Max-p.Mean
	xMin, xMax := -0.5, 0.5
	height := p.getEdgeY()
	y := float64(0)
	if neg < pos {
		y = -neg * height / pos
	} else {
		y = pos * height / neg
	}
	x := p.calcNormX(y, 1e-6, 1000)
	if y > 0 {
		xMax = x
	} else {
		xMin = x
	}
	return xMin, xMax
}

func (p *Param) norm(x float64) float64 {
	return math.Pow(x, 3) + x/(p.Rate*20)
}

/*
norm的导数
*/
func (p *Param) dNorm(x float64) float64 {
	return 3*math.Pow(x, 2) + 1/(p.Rate*20)
}

/*
计算y=pow(x, 3) + x/20当给定y时，x的值
*/
func (p *Param) calcNormX(y, tol float64, maxIter int) float64 {
	x := float64(0)
	for i := 0; i < maxIter; i++ {
		x = x - (p.norm(x)-y)/p.dNorm(x)
		if math.Abs(p.norm(x)-y) < tol {
			return x
		}
	}
	return x
}

func IsLimitOrder(t int) bool {
	return t == OrderTypeLimit || t == OrderTypeLimitMaker
}
