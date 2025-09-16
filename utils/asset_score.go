package utils

import (
	"math"
)

// CalcAssetActivityScore 计算活跃度分数，订单太少，或者长时间无订单的得分低
func CalcAssetActivityScore(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	chgNum := 0
	cumChg, sumVal := float64(0), float64(0)
	for i, v := range data {
		if i == 0 {
			continue
		}
		sumVal += v
		chgVal := math.Abs(v - data[i-1])
		if chgVal > 0 {
			chgNum += 1
			cumChg += chgVal
		}
	}
	// 累计变动分数
	chgScore := float64(0)
	if sumVal > 0 {
		chgScore = cumChg / (sumVal / float64(len(data)))
	}
	// 变动个数分数
	countScore := float64(chgNum) / float64(len(data))
	return countScore*0.5 + chgScore*0.5
}

func CalcAssetStabilityScore32(values []float32, method int) float64 {
	inputs := make([]float64, len(values))
	for i, v := range values {
		inputs[i] = float64(v)
	}
	return CalcAssetStabilityScore(inputs, method)
}

// CalcAssetStabilityScore 计算稳定性分数[-1~1]，method=0自动，1仅回撤，2仅线性拟合
func CalcAssetStabilityScore(values []float64, method int) float64 {
	if method < 0 || method > 2 {
		panic("invalid method for CalcAssetStabilityScore")
	}
	var linearScore, drawDownScore, weiSum float64
	var linearWei, drawDownWei = 0.4, 0.6
	if method != 1 {
		linearScore = CalcAssetLinearScore(values)
		weiSum += linearWei
	}
	if method != 2 {
		drawDownScore = CalcAssetDrawDownScore(values)
		weiSum += drawDownWei
	}
	return (linearScore*linearWei + drawDownScore*drawDownWei) / (weiSum)
}

// CalcAssetLinearScore 计算线性回归稳定性分数[-1~1]，0表示无效
func CalcAssetLinearScore(values []float64) float64 {
	if len(values) < 2 {
		return 0.0
	}
	dirt := -1.0
	if values[len(values)-1] > values[0] {
		dirt = 1
	}

	n := float64(len(values))

	// 计算线性回归参数
	var sumX, sumY, sumXX, sumXY float64
	for i, v := range values {
		x := float64(i)
		y := v
		sumX += x
		sumY += y
		sumXX += x * x
		sumXY += x * y
	}

	// 计算回归系数
	denominator := n*sumXX - sumX*sumX
	if math.Abs(denominator) < 1e-10 {
		return 0.0
	}

	// 计算回归直线斜率和截距
	slope := (n*sumXY - sumX*sumY) / denominator
	intercept := (sumY - slope*sumX) / n

	// 计算总平方和（TSS）和残差平方和（RSS）
	meanY := sumY / n
	var tss, rss float64
	for i, v := range values {
		x := float64(i)
		y := v
		predicted := slope*x + intercept
		tss += (y - meanY) * (y - meanY)
		rss += (y - predicted) * (y - predicted)
	}

	// 计算R²决定系数作为稳定性指标
	if math.Abs(tss) < 1e-10 {
		// 如果所有值都相同，认为完全稳定
		return dirt
	}

	rSquared := 1 - (rss / tss)

	// 确保结果在0.0-1.0范围内
	if rSquared < 0.0 {
		return 0.0
	}
	if rSquared > 1.0 {
		return dirt
	}
	return rSquared * dirt
}

// CalcAssetDrawDownScore 计算资产净值曲线稳定性分数；返回[-1,1]，1表示一路向上，-1表示一路向下
func CalcAssetDrawDownScore(values []float64) float64 {
	if len(values) <= 2 {
		return 0
	}

	minVal, maxVal := values[0], values[0]
	for _, v := range values {
		minVal = min(minVal, v)
		maxVal = max(maxVal, v)
	}
	if minVal < 0 || maxVal > 1 {
		values = NormalizeFloat64(values, 0.3)
	}

	// 判断趋势：上升或下降
	dirt := float64(-1)
	if values[0] <= values[len(values)-1] {
		dirt = 1
	}

	// 计算期望值序列
	predicts := calcPredicts(values, dirt, 0.5)

	// 计算score序列
	drawdowns := make([]float64, len(values))
	for i := range drawdowns {
		val := math.Abs(values[i]-predicts[i]) / 0.5
		drawdowns[i] = 1 - math.Max(0, math.Min(1, val))
	}

	riskScore := computeRiskScore(drawdowns)
	return riskScore * dirt
}

// computeRiskScore 计算风险分数，基于drawdowns序列
// 使用均方误差(MSE)计算风险分数
// 返回值范围[0,1]，0表示无风险，1表示高风险
func computeRiskScore(drawdowns []float64) float64 {
	if len(drawdowns) < 2 {
		return 0.0
	}

	n := float64(len(drawdowns))

	// 计算平均值
	var sum float64
	for _, dd := range drawdowns {
		sum += dd
	}
	mean := sum / n

	// 如果所有drawdown值都很小，风险很低
	if mean < 0.01 {
		return 0.0
	}

	// 计算均方误差 (MSE)
	var squaredSum float64
	for _, dd := range drawdowns {
		squaredSum += dd * dd
	}
	mse := squaredSum / n

	// 将MSE标准化到[0,1]范围
	// MSE的值域是[0, 1]（因为drawdowns已经被限制在[0,1]范围内）
	// 直接使用MSE作为风险分数
	riskScore := math.Min(1.0, math.Max(0.0, mse))

	return riskScore
}

// extremePoint 极值点结构
type extremePoint struct {
	index int
	value float64
}

// calcPredicts 计算期望，smoRate[0-1]其中0表示直接用新高作为期望不线性插值，1表示对新高进行线性插值
func calcPredicts(values []float64, dirt, smoRate float64) []float64 {
	n := len(values)
	predicts := make([]float64, n)

	var extremePoints = findNewExtremes(values, dirt)

	// 在极值点之间进行线性插值
	for j := 0; j < len(extremePoints)-1; j++ {
		x0, y0 := extremePoints[j].index, extremePoints[j].value
		x1, y1Raw := extremePoints[j+1].index, extremePoints[j+1].value
		y1 := y0 + (y1Raw-y0)*smoRate
		predicts[x0] = y0
		predicts[x1] = y1Raw

		for i := x0 + 1; i < x1 && i < n; i++ {
			ratio := float64(i-x0) / float64(x1-x0)
			predicts[i] = y0 + (y1-y0)*ratio
		}
	}

	return predicts
}

// findNewHighsImproved 找出所有新高点
func findNewExtremes(values []float64, dirt float64) []extremePoint {
	var points []extremePoint
	if len(values) == 0 {
		panic("len of values must > 0")
	}

	extremeSoFar := values[0]
	points = append(points, extremePoint{index: 0, value: values[0]})

	for i := 1; i < len(values); i++ {
		if (values[i]-extremeSoFar)*dirt > 0 {
			extremeSoFar = values[i]
			points = append(points, extremePoint{index: i, value: values[i]})
		}
	}

	last := &points[len(points)-1]
	points = append(points, extremePoint{index: len(values) - 1, value: last.value})

	return points
}

// NormalizeFloat64 归一化到minBtm-1范围
func NormalizeFloat64(values []float64, minBtm float64) []float64 {
	if len(values) == 0 {
		return values
	}
	if minBtm > 0.9 || minBtm < 0 {
		panic("minBtm for NormalizeFloat64 should be in [0,0.9]")
	}

	// 查找最大值和最小值
	minVal, maxVal := values[0], values[0]
	for _, v := range values {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	// 如果最大值等于最小值，返回全零切片
	if maxVal == minVal {
		result := make([]float64, len(values))
		return result
	}

	// 归一化到minBtm-1范围
	result := make([]float64, len(values))
	range_ := maxVal - minVal
	scaleRate := 1/(1+minBtm) - 1e5
	for i, v := range values {
		result[i] = ((v-minVal)/range_ + minBtm) * scaleRate
	}

	return result
}
