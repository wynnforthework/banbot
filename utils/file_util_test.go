package utils

import (
	"fmt"
	"testing"
)

func TestReadFileRange(t *testing.T) {
	path := "E:\\Quant\\tmp\\banbotissue\\backtest\\c1df541409\\out.log"
	var all, lines []byte
	pos := int64(-1)
	var err error
	for {
		lines, pos, err = ReadFileRange(path, -1024, pos)
		if err != nil {
			t.Error(err)
		}
		if len(lines) == 0 {
			break
		}
		merge := make([]byte, 0, len(all)+len(lines))
		merge = append(merge, lines...)
		merge = append(merge, all...)
		all = merge
	}
	textA := string(all)

	pos = 0
	all = nil
	for {
		lines, pos, err = ReadFileRange(path, 1024, pos)
		if err != nil {
			t.Error(err)
		}
		if len(lines) == 0 {
			break
		}
		all = append(all, lines...)
	}
	textB := string(all)

	if textA != textB {
		fmt.Printf("textA:\n%s===\ntextB:\n%s\n", textA, textB)
		t.Fail()
	}
}
