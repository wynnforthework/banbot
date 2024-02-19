package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	args := CmdArgs{}
	err := LoadConfig(&args)
	if err != nil {
		fmt.Printf("load data error: %s", err)
		return
	}
	data, err2 := yaml.Marshal(Data)
	if err2 != nil {
		fmt.Printf("dump data error: %s", err2)
		return
	}
	fmt.Println("result: \n", string(data))
}
