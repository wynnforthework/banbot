package btime

import (
	"fmt"
	"testing"
	"time"
)

func TestNow(t *testing.T) {
	tm := time.Now()
	fmt.Printf("time: %v", tm.Unix())
}
