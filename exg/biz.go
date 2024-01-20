package exg

import (
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/bex"
	"github.com/banbox/banexg/errs"
	"strconv"
)

func Setup() *errs.Error {
	if DefExg != nil {
		return nil
	}
	exgCfg := config.Exchange
	if exgCfg == nil {
		return errs.NewMsg(core.ErrBadConfig, "exchange is required")
	}
	var err *errs.Error
	DefExg, err = GetWith(exgCfg.Name, config.MarketType, config.ContractType)
	return err
}

func create(name, market, contractType string) (banexg.BanExchange, *errs.Error) {
	exgCfg := config.Exchange
	if exgCfg == nil {
		return nil, errs.NewMsg(core.ErrBadConfig, "exchange is required")
	}
	cfg, ok := exgCfg.Items[name]
	if !ok {
		return nil, errs.NewMsg(core.ErrBadConfig, "exchange config not found: %s", exgCfg.Name)
	}
	var accounts map[string]*config.CreditConfig
	if core.EnvProd() {
		accounts = cfg.CreditProds
	} else {
		accounts = cfg.CreditTests
	}
	var options = map[string]interface{}{}
	for key, val := range cfg.Options {
		options[utils.SnakeToCamel(key)] = val
	}
	if len(accounts) > 0 {
		accs := map[string]map[string]interface{}{}
		for name, acc := range accounts {
			accs[name] = map[string]interface{}{
				banexg.OptApiKey:    acc.APIKey,
				banexg.OptApiSecret: acc.APISecret,
			}
		}
		options[banexg.OptAccCreds] = accs
	}
	if market != "" {
		options[banexg.OptMarketType] = market
	}
	if contractType != "" {
		options[banexg.OptContractType] = contractType
	}
	return bex.New(name, &options)
}

func Get() (banexg.BanExchange, *errs.Error) {
	exgCfg := config.Exchange
	if exgCfg == nil {
		return nil, errs.NewMsg(core.ErrBadConfig, "exchange is required")
	}
	return GetWith(exgCfg.Name, config.MarketType, config.ContractType)
}

func GetWith(name, market, contractType string) (banexg.BanExchange, *errs.Error) {
	if contractType == "" {
		contractType = config.ContractType
	}
	cacheKey := name + "@" + market + "@" + contractType
	client, ok := exgMap[cacheKey]
	var err *errs.Error
	if !ok {
		client, err = create(name, market, contractType)
		if err != nil {
			return nil, err
		}
		exgMap[cacheKey] = client
	} else {
		err = client.SetMarketType(market, contractType)
	}
	return client, err
}

func PrecCost(exchange banexg.BanExchange, symbol string, cost float64) (float64, *errs.Error) {
	if exchange == nil {
		if DefExg == nil {
			return 0, errs.NewMsg(core.ErrExgNotInit, "exchange not loaded")
		}
		exchange = DefExg
	}
	market, err := exchange.GetMarket(symbol)
	if err != nil {
		return 0, err
	}
	text, err := exchange.PrecCost(market, cost)
	if err != nil {
		return 0, err
	}
	res, err2 := strconv.ParseFloat(text, 64)
	if err2 != nil {
		return 0, errs.New(errs.CodePrecDecFail, err2)
	}
	return res, nil
}
