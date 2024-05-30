package utils

import (
	"errors"
	"github.com/shopspring/decimal"
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

// DecSortinoRatio returns sortino ratio of backtest compared to risk-free
func DecSortinoRatio(moReturns []decimal.Decimal, riskFree, average decimal.Decimal) (decimal.Decimal, error) {
	if len(moReturns) == 0 {
		return decimal.Zero, errZeroValue
	}
	totNegSqrt := decimal.Zero
	val2 := decimal.NewFromInt(2)
	for x := range moReturns {
		ret := moReturns[x].Sub(riskFree)
		if ret.LessThan(decimal.Zero) {
			totNegSqrt = totNegSqrt.Add(ret.Pow(val2))
		}
	}
	if totNegSqrt.IsZero() {
		return decimal.Zero, ErrNoNegativeResults
	}
	f, _ := totNegSqrt.Float64()
	fAvgDownDev := math.Sqrt(f / float64(len(moReturns)))
	avgDownDev := decimal.NewFromFloat(fAvgDownDev)

	return average.Sub(riskFree).Div(avgDownDev), nil
}

// DecSharpeRatio returns sharpe ratio of backtest compared to risk-free
func DecSharpeRatio(moReturns []decimal.Decimal, riskFree, average decimal.Decimal) (decimal.Decimal, error) {
	totalIntervals := decimal.NewFromInt(int64(len(moReturns)))
	if totalIntervals.IsZero() {
		return decimal.Zero, errZeroValue
	}
	excessReturns := make([]decimal.Decimal, len(moReturns))
	for i := range moReturns {
		excessReturns[i] = moReturns[i].Sub(riskFree)
	}
	stdDev, err := DecStdDev(excessReturns)
	if err != nil {
		return decimal.Zero, err
	}
	if stdDev.IsZero() {
		return decimal.Zero, nil
	}

	return average.Sub(riskFree).Div(stdDev), nil
}
