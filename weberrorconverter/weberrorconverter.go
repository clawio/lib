package weberrorconverter

import (
	"encoding/json"
	"github.com/clawio/lib"
)

type converter struct{}

func New() lib.WebErrorConverter {
	return &converter{}
}

func (c *converter) ErrorToJSON(err error) ([]byte, error) {
	jsonErr := &jsonError{}
	ourError, ok := err.(lib.Error)
	if ok {
		jsonErr.Code = ourError.Code()
		jsonErr.Message = ourError.Message()
	} else {
		jsonErr.Code = lib.CodeInternal
		jsonErr.Message = "something went really bad"
	}
	return json.Marshal(jsonErr)
}

type jsonError struct {
	Code    lib.Code `json:"code"`
	Message string `json:"message"`
}
