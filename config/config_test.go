package config

import (
	"fmt"
	"github.com/anyongjin/gobanbot/cmd"
	"gopkg.in/yaml.v3"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	args := cmd.CmdArgs{}
	cfg, err := LoadConfig(&args)
	if err != nil {
		fmt.Printf("load config error: %s", err)
		return
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		fmt.Printf("dump config error: %s", err)
		return
	}
	fmt.Println("result: \n", string(data))
}
