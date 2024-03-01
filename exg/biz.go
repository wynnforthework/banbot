package exg

import (
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/bex"
	"github.com/banbox/banexg/errs"
)

func Setup() *errs.Error {
	if Default != nil {
		return nil
	}
	exgCfg := config.Exchange
	if exgCfg == nil {
		return errs.NewMsg(core.ErrBadConfig, "exchange is required")
	}
	var err *errs.Error
	Default, err = GetWith(exgCfg.Name, core.Market, core.ContractType)
	core.IsContract = banexg.IsContract(core.Market)
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

func GetWith(name, market, contractType string) (banexg.BanExchange, *errs.Error) {
	if contractType == "" {
		contractType = core.ContractType
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

func precNum(exchange banexg.BanExchange, symbol string, num float64, source string) (float64, *errs.Error) {
	if exchange == nil {
		if Default == nil {
			return 0, errs.NewMsg(core.ErrExgNotInit, "exchange not loaded")
		}
		exchange = Default
	}
	market, err := exchange.GetMarket(symbol)
	if err != nil {
		return 0, err
	}
	var res float64
	if source == "cost" {
		res, err = exchange.PrecCost(market, num)
	} else if source == "price" {
		res, err = exchange.PrecPrice(market, num)
	} else if source == "amount" {
		res, err = exchange.PrecAmount(market, num)
	} else if source == "fee" {
		res, err = exchange.PrecFee(market, num)
	} else {
		panic("invalid source to prec: " + source)
	}
	return res, err
}

func PrecCost(exchange banexg.BanExchange, symbol string, cost float64) (float64, *errs.Error) {
	return precNum(exchange, symbol, cost, "cost")
}

func PrecPrice(exchange banexg.BanExchange, symbol string, price float64) (float64, *errs.Error) {
	return precNum(exchange, symbol, price, "price")
}

func PrecAmount(exchange banexg.BanExchange, symbol string, amount float64) (float64, *errs.Error) {
	return precNum(exchange, symbol, amount, "amount")
}

func GetLeverage(symbol string, notional float64) (int, int) {
	leverage, maxVal := Default.GetLeverage(symbol, notional)
	if leverage == 0 {
		leverage = config.Leverage
	}
	return leverage, maxVal
}

func GetOdBook(pair string) (*banexg.OrderBook, *errs.Error) {
	book, ok := core.OdBooks[pair]
	if !ok || book == nil || book.TimeStamp+config.OdBookTtl < btime.TimeMS() {
		var err *errs.Error
		book, err = Default.FetchOrderBook(pair, 1000, nil)
		if err != nil {
			return nil, err
		}
		core.OdBooks[pair] = book
	}
	return book, nil
}
