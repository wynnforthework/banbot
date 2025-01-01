package ormo

import (
	"encoding/gob"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	"go.uber.org/zap"
	"math"
	"os"

	"github.com/banbox/banexg/errs"
)

func DumpOrdersGob(path string) *errs.Error {
	file, err := os.Create(path)
	if err != nil {
		return errs.New(errs.CodeIOWriteFail, err)
	}
	defer file.Close()

	for _, od := range HistODs {
		_, _ = od.GetInfoText()
		od.Info = nil
	}
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(HistODs)
	if err != nil {
		return errs.New(errs.CodeIOWriteFail, err)
	}
	return nil
}

func LoadOrdersGob(path string) ([]*InOutOrder, *errs.Error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errs.New(errs.CodeIOReadFail, err)
	}
	defer file.Close()

	var data []*InOutOrder
	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&data)
	if err != nil {
		return nil, errs.New(errs.CodeIOReadFail, err)
	}
	return data, nil
}

func decodeIOrderInfo(text string) map[string]interface{} {
	result := make(map[string]interface{})
	if text == "" {
		return result
	}
	err_ := utils2.UnmarshalString(text, &result, utils2.JsonNumDefault)
	if err_ != nil {
		log.Error("unmarshal ioder info fail", zap.String("info", text), zap.Error(err_))
	} else {
		for key, val := range result {
			if key == OdInfoStopAfter {
				if floatVal, ok := val.(float64); ok {
					result[key] = int64(math.Round(floatVal))
				}
			} else if key == OdInfoStopLoss || key == OdInfoTakeProfit {
				if mapVal, ok := val.(map[string]interface{}); ok {
					state := decodeTriggerState(mapVal)
					if state == nil {
						delete(result, key)
					} else {
						result[key] = state
					}
				}
			}
		}
	}
	return result
}

func decodeTriggerState(data map[string]interface{}) *TriggerState {
	if data == nil || len(data) == 0 {
		return nil
	}
	ts := &TriggerState{
		ExitTrigger: decodeTrigger(data),
	}
	if v, ok := data["range"].(float64); ok {
		ts.Range = v
	}
	if v, ok := data["hit"].(bool); ok {
		ts.Hit = v
	}
	if v, ok := data["order_id"].(string); ok {
		ts.OrderId = v
	}

	// 处理嵌套的Old字段
	if oldData, ok := data["old"].(map[string]interface{}); ok {
		ts.Old = decodeTrigger(oldData)
	}

	return ts
}

func decodeTrigger(data map[string]interface{}) *ExitTrigger {
	ts := &ExitTrigger{}
	if v, ok := data["price"].(float64); ok {
		ts.Price = v
	}
	if v, ok := data["limit"].(float64); ok {
		ts.Limit = v
	}
	if v, ok := data["rate"].(float64); ok {
		ts.Rate = v
	}
	if v, ok := data["tag"].(string); ok {
		ts.Tag = v
	}
	return ts
}
