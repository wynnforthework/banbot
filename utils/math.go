package utils

import (
	"errors"
	"github.com/shopspring/decimal"
	"gonum.org/v1/gonum/stat"
	"math"
)

var (
	// ErrNoNegativeResults is returned when no negative results are allowed
	ErrNoNegativeResults       = errors.New("cannot calculate with no negative values")
	errZeroValue               = errors.New("cannot calculate average of no values")
	errNegativeValueOutOfRange = errors.New("received negative number less than -1")
)

// DecPow is lovely because shopspring decimal cannot
// handle ^0.x and instead returns 1
func DecPow(x, y decimal.Decimal) decimal.Decimal {
	pow := math.Pow(x.InexactFloat64(), y.InexactFloat64())
	if math.IsNaN(pow) || math.IsInf(pow, 0) {
		return decimal.Zero
	}
	return decimal.NewFromFloat(pow)
}

// DecArithMean is the basic form of calculating an average.
// Divide the sum of all values by the length of values
func DecArithMean(values []decimal.Decimal) (decimal.Decimal, error) {
	if len(values) == 0 {
		return decimal.Zero, errZeroValue
	}
	var sumOfValues decimal.Decimal
	for _, v := range values {
		sumOfValues = sumOfValues.Add(v)
	}
	return sumOfValues.Div(decimal.NewFromInt(int64(len(values)))), nil
}

// DecStdDev calculates standard deviation using population based calculation
func DecStdDev(values []decimal.Decimal) (decimal.Decimal, error) {
	if len(values) < 2 {
		return decimal.Zero, nil
	}
	valAvg, err := DecArithMean(values)
	if err != nil {
		return decimal.Zero, err
	}
	diffs := make([]decimal.Decimal, len(values))
	exp := decimal.NewFromInt(2)
	for x := range values {
		val := values[x].Sub(valAvg)
		diffs[x] = DecPow(val, exp)
	}
	var diffAvg decimal.Decimal
	diffAvg, err = DecArithMean(diffs)
	if err != nil {
		return decimal.Zero, err
	}
	f, _ := diffAvg.Float64()
	resp := decimal.NewFromFloat(math.Sqrt(f))
	return resp, nil
}

// DecFinaGeomMean is a modified geometric average to assess
// the negative returns of investments. It accepts It adds +1 to each
// This does impact the final figures as it is modifying values
// It is still ultimately calculating a geometric average
// which should only be compared to other financial geometric averages
func DecFinaGeomMean(values []decimal.Decimal) (decimal.Decimal, error) {
	if len(values) == 0 {
		return decimal.Zero, errZeroValue
	}
	product := 1.0
	for i := range values {
		if values[i].LessThan(decimal.NewFromInt(-1)) {
			// cannot lose more than 100%, figures are incorrect
			// losing exactly 100% will return a 0 value, but is not an error
			return decimal.Zero, errNegativeValueOutOfRange
		}
		// as we cannot have negative or zero value geometric numbers
		// adding a 1 to the percentage movements allows for differentiation between
		// negative numbers (eg -0.1 translates to 0.9) and positive numbers (eg 0.1 becomes 1.1)
		modVal := values[i].Add(decimal.NewFromInt(1)).InexactFloat64()
		product *= modVal
	}
	prod := 1 / float64(len(values))
	geometricPower := math.Pow(product, prod)
	if geometricPower > 0 {
		// we minus 1 because we manipulated the values to be non-zero/negative
		geometricPower--
	}
	return decimal.NewFromFloat(geometricPower), nil
}

func SortinoRatio(moReturns []float64, riskFree float64) (float64, error) {
	return SortinoRatioAdv(moReturns, riskFree, 252, true, false)
}

func SortinoRatioBy(moReturns []float64, riskFree float64, periods int, annualize bool) (float64, error) {
	return SortinoRatioAdv(moReturns, riskFree, periods, annualize, false)
}

func SortinoRatioSmart(moReturns []float64, riskFree float64, periods int, annualize bool) (float64, error) {
	return SortinoRatioAdv(moReturns, riskFree, periods, annualize, true)
}

func SortinoRatioAdv(moReturns []float64, riskFree float64, periods int, annualize, smart bool) (float64, error) {
	res, err := DecSortinoRatioAdv(FloatsToDecArr(moReturns), decimal.NewFromFloat(riskFree), periods, annualize, smart)
	flt, _ := res.Float64()
	return flt, err
}

func DecSortinoRatio(moReturns []decimal.Decimal, riskFree decimal.Decimal) (decimal.Decimal, error) {
	return DecSortinoRatioAdv(moReturns, riskFree, 252, true, false)
}

func DecSortinoRatioBy(moReturns []decimal.Decimal, riskFree decimal.Decimal, periods int, annualize bool) (decimal.Decimal, error) {
	return DecSortinoRatioAdv(moReturns, riskFree, periods, annualize, false)
}

func DecSortinoRatioSmart(moReturns []decimal.Decimal, riskFree decimal.Decimal, periods int, annualize bool) (decimal.Decimal, error) {
	return DecSortinoRatioAdv(moReturns, riskFree, periods, annualize, true)
}

// DecSortinoRatioAdv returns sortino ratio of backtest compared to risk-free
func DecSortinoRatioAdv(moReturns []decimal.Decimal, riskFree decimal.Decimal, periods int, annualize, smart bool) (decimal.Decimal, error) {
	if len(moReturns) == 0 {
		return decimal.Zero, errZeroValue
	}
	if !riskFree.Equal(decimal.Zero) && periods <= 0 {
		return decimal.Zero, errors.New("must provide periods if riskFree!=0")
	}
	// Calculates excess returns by subtracting risk-free returns from total returns
	excessReturns, avg := prepareExcessReturns(moReturns, riskFree, periods)

	totNegSqrt := decimal.Zero
	val2 := decimal.NewFromInt(2)
	for _, ret := range excessReturns {
		if ret.LessThan(decimal.Zero) {
			totNegSqrt = totNegSqrt.Add(ret.Pow(val2))
		}
	}
	if totNegSqrt.IsZero() {
		return decimal.Zero, ErrNoNegativeResults
	}
	downSide := totNegSqrt.Div(decimal.NewFromInt32(int32(len(excessReturns)))).Pow(decimal.NewFromFloat(0.5))
	if smart {
		downSide = downSide.Mul(decimal.NewFromFloat(AutoCorrPenalty(DecArrToFloats(excessReturns))))
	}
	if downSide.Equal(decimal.Zero) {
		return decimal.Zero, nil
	}
	res := avg.Div(downSide)
	if annualize && periods > 1 {
		res = res.Mul(decimal.NewFromFloat(math.Sqrt(float64(periods))))
	}
	return res, nil
}

func SharpeRatio(moReturns []float64, riskFree float64) (float64, error) {
	return SharpeRatioAdv(moReturns, riskFree, 252, true, false)
}

func SharpeRatioBy(moReturns []float64, riskFree float64, periods int, annualize bool) (float64, error) {
	return SharpeRatioAdv(moReturns, riskFree, periods, annualize, false)
}

func SharpeRatioSmart(moReturns []float64, riskFree float64, periods int, annualize bool) (float64, error) {
	return SharpeRatioAdv(moReturns, riskFree, periods, annualize, true)
}

/*
SharpeRatioAdv 计算夏普比率

moReturns 固定周期的收益率，一般用日收益率
riskFree 年华无风险收益率，不为0时periods必填
periods 一年中moReturns所用的周期总数量，对日收益率，股票市场一般252
smart true时启用相关性惩罚
*/
func SharpeRatioAdv(moReturns []float64, riskFree float64, periods int, annualize, smart bool) (float64, error) {
	res, err := DecSharpeRatioAdv(FloatsToDecArr(moReturns), decimal.NewFromFloat(riskFree), periods, annualize, smart)
	flt, _ := res.Float64()
	return flt, err
}

// DecSharpeRatio use 252 as default periods to calculate annualize
func DecSharpeRatio(moReturns []decimal.Decimal, riskFree decimal.Decimal) (decimal.Decimal, error) {
	return DecSharpeRatioAdv(moReturns, riskFree, 252, true, false)
}

func DecSharpeRatioBy(moReturns []decimal.Decimal, riskFree decimal.Decimal, periods int, annualize bool) (decimal.Decimal, error) {
	return DecSharpeRatioAdv(moReturns, riskFree, periods, annualize, false)
}

func DecSharpeRatioSmart(moReturns []decimal.Decimal, riskFree decimal.Decimal, periods int, annualize bool) (decimal.Decimal, error) {
	return DecSharpeRatioAdv(moReturns, riskFree, periods, annualize, true)
}

/*
DecSharpeRatioAdv 计算夏普比率

moReturns 固定周期的收益率，一般用日收益率
riskFree 年华无风险收益率，不为0时periods必填
periods 一年中moReturns所用的周期总数量，对日收益率，股票市场一般252
smart true时启用相关性惩罚
*/
func DecSharpeRatioAdv(moReturns []decimal.Decimal, riskFree decimal.Decimal, periods int, annualize, smart bool) (decimal.Decimal, error) {
	totalIntervals := decimal.NewFromInt(int64(len(moReturns)))
	if totalIntervals.IsZero() {
		return decimal.Zero, errZeroValue
	}
	if !riskFree.Equal(decimal.Zero) && periods <= 0 {
		return decimal.Zero, errors.New("must provide periods if riskFree!=0")
	}
	// Calculates excess returns by subtracting risk-free returns from total returns
	excessReturns, avg := prepareExcessReturns(moReturns, riskFree, periods)
	stdDev, err := DecStdDev(excessReturns)
	if err != nil {
		return decimal.Zero, err
	}
	if stdDev.IsZero() {
		return decimal.Zero, nil
	}
	if smart {
		stdDev = stdDev.Mul(decimal.NewFromFloat(AutoCorrPenalty(DecArrToFloats(excessReturns))))
	}

	res := avg.Div(stdDev)
	if annualize && periods > 1 {
		res = res.Mul(decimal.NewFromFloat(math.Sqrt(float64(periods))))
	}
	return res, nil
}

func prepareExcessReturns(arr []decimal.Decimal, riskFree decimal.Decimal, periods int) ([]decimal.Decimal, decimal.Decimal) {
	var excessReturns []decimal.Decimal
	var avg decimal.Decimal
	if periods > 0 {
		dec1 := decimal.NewFromFloat(1)
		riskFree = riskFree.Add(dec1).Pow(dec1.Div(decimal.NewFromInt(int64(periods)))).Sub(dec1)
		excessReturns = make([]decimal.Decimal, len(arr))
		for i := range arr {
			excessReturns[i] = arr[i].Sub(riskFree)
			avg = avg.Add(excessReturns[i])
		}
	} else {
		for i := range arr {
			avg = avg.Add(arr[i])
		}
		excessReturns = arr
	}
	avg = avg.Div(decimal.NewFromInt32(int32(len(excessReturns))))
	return excessReturns, avg
}

func DecArrToFloats(arr []decimal.Decimal) []float64 {
	var result = make([]float64, 0, len(arr))
	for _, v := range arr {
		fltV, _ := v.Float64()
		result = append(result, fltV)
	}
	return result
}

func FloatsToDecArr(arr []float64) []decimal.Decimal {
	var result = make([]decimal.Decimal, 0, len(arr))
	for _, v := range arr {
		result = append(result, decimal.NewFromFloat(v))
	}
	return result
}

// AutoCorrPenalty Metric to account for auto correlation
func AutoCorrPenalty(returns []float64) float64 {
	num := len(returns)

	// 计算序列 returns[:-1] 和 returns[1:] 的相关系数
	returns1 := returns[:num-1]
	returns2 := returns[1:]
	coef := math.Abs(stat.Correlation(returns1, returns2, nil))

	// 计算corr列表
	var corr []float64
	for x := 1; x < num; x++ {
		val := (float64(num-x) / float64(num)) * math.Pow(coef, float64(x))
		corr = append(corr, val)
	}

	// 计算 sqrt(1 + 2 * sum(corr))
	sumCorr := 0.0
	for _, c := range corr {
		sumCorr += c
	}

	result := math.Sqrt(1 + 2*sumCorr)
	return result
}
