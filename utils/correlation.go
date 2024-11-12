package utils

import (
	"bytes"
	"errors"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	ta "github.com/banbox/banta"
	"github.com/fogleman/gg"
	"go.uber.org/zap"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"gonum.org/v1/gonum/mat"
	"gonum.org/v1/gonum/stat"
	"image/color"
	"image/png"
	"math"
	"strconv"
)

/*
CalcCorrMat calculate Correlation Matrix for given data
*/
func CalcCorrMat(arrLen int, dataArr [][]float64, useChgRate bool) (*mat.SymDense, []float64, error) {
	if len(dataArr) <= 1 {
		return nil, nil, errors.New("at least two data series are required to calculate correlation")
	}
	if useChgRate {
		resArr := make([][]float64, 0, len(dataArr))
		for _, arr := range dataArr {
			lnPfts := make([]float64, 0, len(arr)-1)
			for i, b := range arr[1:] {
				pft := b/arr[i] - 1
				lnPft := math.Log1p(math.Abs(pft))
				if pft < 0 {
					lnPft = -lnPft
				}
				lnPfts = append(lnPfts, lnPft)
			}
			resArr = append(resArr, lnPfts)
		}
		dataArr = resArr
	}
	// calculate CorrelationMatrix
	numAssets := len(dataArr)
	for i, col := range dataArr {
		curLen := len(col)
		if curLen > arrLen {
			msg := "col %v length %v should <= arrLen %v"
			return nil, nil, errs.NewMsg(errs.CodeParamInvalid, msg, i, curLen, arrLen)
		} else if curLen < arrLen {
			dataArr[i] = append(make([]float64, arrLen-curLen), col...)
		}
	}
	matrixData := make([]float64, 0, numAssets*arrLen)
	for i := 0; i < arrLen; i++ {
		for j := 0; j < numAssets; j++ {
			matrixData = append(matrixData, dataArr[j][i])
		}
	}
	// Construct a return matrix, where each column is the return sequence of an asset
	returnsMat := mat.NewDense(arrLen, numAssets, matrixData)
	corrMat := mat.NewSymDense(numAssets, nil)
	stat.CorrelationMatrix(corrMat, returnsMat, nil)
	avgs := make([]float64, 0, numAssets)
	for i := 0; i < numAssets; i++ {
		var sum float64
		for j := 0; j < numAssets; j++ {
			if i == j {
				continue
			}
			sum += corrMat.At(i, j)
		}
		avgs = append(avgs, sum/float64(numAssets-1))
	}
	return corrMat, avgs, nil
}

func GenCorrImg(m *mat.SymDense, title string, names []string, fontName string, fontSize float64) ([]byte, error) {
	const lenPadding = 80 // pix length of names
	const lenTitle = 80
	const lenColorBar = 100
	const minWidth = 300
	const minCellSize = 40
	rows, cols := m.Dims()
	matLen := max(rows, cols)
	var matWidth = minCellSize * matLen
	if matWidth < minWidth {
		matWidth = minWidth
	}
	var cellSize = int(math.Round(float64(matWidth) / float64(matLen)))
	matWidth = cellSize * matLen
	imgWidth := matWidth + lenColorBar + lenPadding
	imgHeight := matWidth + lenPadding + lenTitle

	dc := gg.NewContext(imgWidth, imgHeight)
	// set background as white
	dc.SetRGB(1, 1, 1)
	dc.Clear()

	// draw title
	dc.SetRGB(0, 0, 0)
	fontFace, err := GetOpenFont(fontName)
	if err != nil {
		log.Warn("load font fail when create CorrMatrixImage", zap.Error(err))
	}
	if fontSize == 0 {
		fontSize = 16
	}
	setFontFace(dc, fontFace, fontSize*1.5, 72)
	dc.DrawStringAnchored(title, float64(imgWidth)/2, float64(lenTitle)/2, 0.5, 0.5)
	setFontFace(dc, fontFace, fontSize, 72)

	// draw hot matrix
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			value := m.At(i, cols-j-1)
			toColor := correlationToColor(value)
			dc.SetColor(toColor)
			x := float64(j*cellSize + lenPadding)
			y := float64(i*cellSize + lenTitle)
			size := float64(cellSize)
			dc.DrawRectangle(x, y, size, size)
			dc.Fill()
			if math.Abs(value) > 0.7 {
				dc.SetRGB(1, 1, 1)
			} else {
				dc.SetRGB(0, 0, 0)
			}
			valStr := strconv.FormatFloat(value, 'f', 2, 64)
			dc.DrawStringAnchored(valStr, x+size/2, y+size/2, 0.5, 0.5)
		}
	}
	// draw names
	dc.SetRGB(0, 0, 0)
	for i, name := range names {
		leftY := float64(i*cellSize + cellSize/2 + lenTitle)
		dc.DrawStringAnchored(name, float64(lenPadding)/2, leftY, 0.5, 0.5)
		bottomX := float64((len(names)-i-1)*cellSize + cellSize/2 + lenPadding)
		dc.Push()
		dc.Translate(bottomX, float64(imgHeight-lenPadding/2))
		dc.Rotate(-math.Pi / 2)
		dc.DrawStringAnchored(name, 0, 0, 0.5, 0.5)
		dc.Pop()
	}
	// draw color bar
	barX := float64(imgWidth - lenColorBar)
	endY := float64(imgHeight - lenPadding)
	midY := (lenTitle + endY) / 2
	grad := gg.NewLinearGradient(barX, lenTitle, barX, midY)
	grad.AddColorStop(0, color.RGBA{B: 255, A: 255}) // blue
	grad.AddColorStop(1, color.White)
	dc.SetFillStyle(grad)
	dc.DrawRectangle(barX+lenColorBar/4, lenTitle, lenColorBar/5, midY-lenTitle)
	dc.Fill()
	dc.SetRGB(0, 0, 0)
	dc.DrawStringAnchored("1", barX+lenColorBar*2/3, lenTitle, 0.5, 1)
	dc.DrawStringAnchored("0", barX+lenColorBar*2/3, midY, 0.5, 0.5)
	dc.DrawStringAnchored("-1", barX+lenColorBar*2/3, endY, 0.5, 0)
	grad = gg.NewLinearGradient(barX, midY, barX, endY)
	grad.AddColorStop(0, color.White)
	grad.AddColorStop(1, color.RGBA{R: 255, A: 255}) // red
	dc.SetFillStyle(grad)
	dc.DrawRectangle(barX+lenColorBar/4, midY, lenColorBar/5, midY-lenTitle)
	dc.Fill()

	var buf bytes.Buffer
	err = png.Encode(&buf, dc.Image())
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func setFontFace(dc *gg.Context, fontFace *opentype.Font, size, dpi float64) {
	if fontFace == nil {
		return
	}
	face, err := opentype.NewFace(fontFace, &opentype.FaceOptions{
		Size:    size,
		DPI:     dpi,
		Hinting: font.HintingNone,
	})
	if err == nil {
		dc.SetFontFace(face)
	} else {
		log.Warn("load font fail when create CorrMatrixImage", zap.Error(err))
	}
}

func correlationToColor(value float64) color.Color {
	// 红蓝过渡，-1为红色，1为蓝色，0为白色
	if value < -1 {
		value = -1
	}
	if value > 1 {
		value = 1
	}
	r := 1.0
	g := 1.0 - math.Abs(value)
	b := 1.0
	if value < 0 {
		b = 1 + value // 越接近-1, 蓝色越少
	} else {
		r = 1 - value // 越接近1，红色越少
	}
	return color.RGBA{uint8(r * 255), uint8(g * 255), uint8(b * 255), 255}
}

func CalcEnvsCorr(envs []*ta.BarEnv, hisNum int) (*mat.SymDense, []float64, error) {
	if len(envs) < 2 {
		return nil, nil, nil
	}
	// the latest timestamp between envs possible inconsistency, so we find the timestamp which is most common
	// 可能envs最新时间戳不一致，查找数量最多的时间戳
	stamps := make(map[int64]int)
	for _, e := range envs {
		count, ok := stamps[e.TimeStart]
		if !ok {
			stamps[e.TimeStart] = 1
		} else {
			stamps[e.TimeStart] = count + 1
		}
	}
	var stamp int64
	var count int
	for st, cnt := range stamps {
		if cnt > count {
			stamp = st
			count = cnt
		}
	}
	// Obtain prices and calculate relevant matrices
	// 获取价格，计算相关矩阵
	data := make([][]float64, 0, len(envs))
	for _, e := range envs {
		offset := max(0, int((e.TimeStart-stamp)/e.TFMSecs))
		data = append(data, e.Close.Range(offset, offset+hisNum))
	}
	return CalcCorrMat(hisNum, data, true)
}
