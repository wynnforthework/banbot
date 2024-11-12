package utils

import (
	"fmt"
	"os"
	"testing"
)

func TestGenCorrImg(t *testing.T) {
	dataArr := [][]float64{
		{0.1, 0.2, 0.01, 0.13, 0.0, -0.05, -0.08, -0.1, 0.1, 0.2},
		{0.01, 0.06, 0.03, 0.03, 0.01, -0.02, -0.01, 0.1, 0.12, 0.04},
		{0.12, 0.03, -0.01, 0.23, 0.14, -0.09, 0.08, -0.01, 0.04, 0.1},
		{0.03, 0.12, 0.04, -0.13, 0.0, -0.01, 0.08, -0.1, 0.06, 0.2},
		{-0.05, 0.1, 0.07, -0.13, 0.06, -0.05, -0.01, -0.06, 0.12, 0.07},
	}
	title := "pearson correlation"
	names := []string{"btc", "eth", "fol", "etc", "eos"}
	corrMat, _, err := CalcCorrMat(len(dataArr[0]), dataArr, false)
	if err != nil {
		panic(err)
	}
	data, err := GenCorrImg(corrMat, title, names, "", 0)
	if err != nil {
		panic(err)
	}
	file, err := os.OpenFile("example.png", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("open file fail:", err)
		return
	}
	defer file.Close()
	_, err = file.Write(data)
	if err != nil {
		fmt.Println("write png data fail:", err)
	}
}
