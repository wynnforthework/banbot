package ormo

import (
	"encoding/gob"
	"github.com/banbox/banexg/errs"
	"os"
)

func DumpOrdersGob(path string) *errs.Error {
	file, err := os.Create(path)
	if err != nil {
		return errs.New(errs.CodeIOWriteFail, err)
	}
	defer file.Close()

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
