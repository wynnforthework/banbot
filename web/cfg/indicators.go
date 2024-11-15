package cfg

import (
	utils2 "github.com/banbox/banexg/utils"
	"github.com/banbox/banta"
	"github.com/gofiber/fiber/v2"
	"math"
	"sort"
	"strconv"
	"strings"
)

type Figure struct {
	Key       string  `json:"key"`   // tr
	Title     string  `json:"title"` // TR:
	Type      string  `json:"type"`  // tag/line
	BaseValue float64 `json:"baseValue"`
}

type DrawInd struct {
	Name       string
	Title      string
	IsMain     bool
	CalcParams []float64 // 参数
	Figures    []*Figure
	FigureTpl  string // 客户端会使用此模板动态生成Figures
	FigureType string // 默认空，客户端默认line
	doCalc     func(e *banta.BarEnv, params []float64) []float64
}

type AdvInd struct {
	*DrawInd
	Calc func(kline [][]float64, params []float64) (interface{}, error)
}

var (
	baseInds = map[string]*DrawInd{
		"RMA": {
			Title:      "RMA",
			IsMain:     true,
			CalcParams: []float64{5, 10, 30},
			FigureTpl:  "rma{period}",
			doCalc: func(e *banta.BarEnv, params []float64) []float64 {
				res := make([]float64, len(params))
				for i, p := range params {
					res[i] = banta.RMA(e.Close, int(p)).Get(0)
				}
				return res
			},
		},
		"TR": {
			Title: "TR 真实振幅",
			Figures: []*Figure{
				{"tr", "TR: ", "line", 0},
			},
			doCalc: func(e *banta.BarEnv, params []float64) []float64 {
				val := banta.TR(e.High, e.Low, e.Close).Get(0)
				return []float64{val}
			},
		},
		"ATR": {
			Title:      "ATR 平均真实振幅",
			CalcParams: []float64{14},
			FigureTpl:  "atr{period}",
			doCalc: func(e *banta.BarEnv, params []float64) []float64 {
				res := make([]float64, len(params))
				for i, p := range params {
					res[i] = banta.ATR(e.High, e.Low, e.Close, int(p)).Get(0)
				}
				return res
			},
		},
		"StdDev": {
			Title:      "StdDev 标准差",
			CalcParams: []float64{7},
			FigureTpl:  "sdev{period}",
			doCalc: func(e *banta.BarEnv, params []float64) []float64 {
				res := make([]float64, len(params))
				for i, p := range params {
					res[i] = banta.StdDev(e.Close, int(p)).Get(0)
				}
				return res
			},
		},
		"TD": {
			Title: "TD 狄马克序列(神奇九转)",
			Figures: []*Figure{
				{"td", "TD: ", "line", 0},
			},
			doCalc: func(e *banta.BarEnv, params []float64) []float64 {
				val := banta.TD(e.Close).Get(0)
				return []float64{val}
			},
		},
		"ADX": {
			Title:      "ADX",
			CalcParams: []float64{14},
			Figures: []*Figure{
				{"adx", "ADX: ", "line", 0},
				{"dip", "DI+: ", "line", 0},
				{"dim", "DI-: ", "line", 0},
			},
			doCalc: func(e *banta.BarEnv, params []float64) []float64 {
				cols := banta.ADX(e.High, e.Low, e.Close, int(params[0])).Cols
				return []float64{cols[0].Get(0), cols[1].Get(0), cols[2].Get(0)}
			},
		},
	}
	advInds = map[string]*AdvInd{
		"ChanLun": {
			DrawInd: &DrawInd{
				Title:  "Chan 缠论",
				IsMain: true,
			},
			Calc: func(kline [][]float64, params []float64) (interface{}, error) {
				cg := &banta.CGraph{}
				for i, k := range kline {
					b := &banta.Kline{
						Time:   int64(k[0]),
						Open:   k[1],
						High:   k[2],
						Low:    k[3],
						Close:  k[4],
						Volume: k[5],
					}
					if len(k) > 6 {
						b.Info = k[6]
					}
					cg.AddBars(i+1, b)
				}
				cg.Parse()
				lines := cg.Dump()
				res := make([][]float64, 0, len(lines))
				for _, l := range lines {
					res = append(res, []float64{float64(l.StartPos), l.StartPrice, float64(l.StopPos), l.StopPrice})
				}
				return res, nil
			},
		},
	}
	IndsCache = make([]map[string]interface{}, 0)
)

func init() {
	for name, ind := range baseInds {
		ind.Name = name
		IndsCache = append(IndsCache, ind.ToMap())
	}
	for name, ind := range advInds {
		ind.Name = name
		IndsCache = append(IndsCache, ind.ToMap())
	}

	sort.Slice(IndsCache, func(i, j int) bool {
		a := IndsCache[i]["name"].(string)
		b := IndsCache[j]["name"].(string)
		return a < b
	})
}

func CalcInd(name string, kline [][]float64, params []float64) (interface{}, error) {
	indi, _ := advInds[name]
	if indi != nil {
		return indi.Calc(kline, params)
	}
	ind, ok := baseInds[name]
	if !ok {
		return nil, &fiber.Error{
			Code:    fiber.StatusBadRequest,
			Message: "unsupported indicator: " + name,
		}
	}
	return ind.Calc(kline, params), nil
}

func (d *DrawInd) Calc(kline [][]float64, params []float64) []map[string]float64 {
	if len(kline) < 2 {
		return nil
	}
	tfMSecs := int64(kline[1][0] - kline[0][0])
	timeFrame := utils2.SecsToTF(int(tfMSecs / 1000))

	var env = &banta.BarEnv{
		TimeFrame:  timeFrame,
		TFMSecs:    tfMSecs,
		Exchange:   "binance",
		MarketType: "linear",
	}
	figures := d.Figures
	if d.FigureTpl != "" {
		if strings.Contains(d.FigureTpl, "{period}") {
			for _, p := range params {
				key := strings.Replace(d.FigureTpl, "{period}", strconv.Itoa(int(p)), 1)
				figures = append(figures, &Figure{Key: key})
			}
		} else {
			figures = append(figures, &Figure{Key: d.FigureTpl})
		}
	}
	res := make([]map[string]float64, 0, len(kline))
	for _, k := range kline {
		var info = float64(0)
		if len(k) > 6 {
			info = k[6]
		}
		env.OnBar(int64(k[0]), k[1], k[2], k[3], k[4], k[5], info)
		arr := d.doCalc(env, params)
		data := make(map[string]float64)
		for i, v := range arr {
			if i >= len(figures) {
				break
			}
			if math.IsInf(v, 0) || math.IsNaN(v) {
				continue
			}
			data[figures[i].Key] = v
		}
		res = append(res, data)
	}
	return res
}

func (d *DrawInd) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"name":        d.Name,
		"title":       d.Title,
		"is_main":     d.IsMain,
		"calcParams":  d.CalcParams,
		"figures":     d.Figures,
		"figure_tpl":  d.FigureTpl,
		"figure_type": d.FigureType,
	}
}
