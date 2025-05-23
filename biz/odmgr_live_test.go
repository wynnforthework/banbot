package biz

import (
	"fmt"
	"github.com/banbox/banbot/config"
	"testing"
)

func TestParseClient(t *testing.T) {
	config.Name = "big"
	res := getClientOrderId("big_1176_747_")
	fmt.Println(res)
}
