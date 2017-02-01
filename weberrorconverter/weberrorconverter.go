package weberrorconverter

import (
	"encoding/json"
	"github.com/clawio/clawiod/root"
)

type converter struct{}

func New() root.WebErrorConverter {
	return &converter{}
}

func (c *converter) ErrorToJSON(err error) ([]byte, error) {
	jsonErr := &jsonError{}
	ourError, ok := err.(root.Error)
	if ok {
		jsonErr.Code = ourError.Code()
		jsonErr.Message = ourError.Message()
	} else {
		jsonErr.Code = root.CodeInternal
		jsonErr.Message = "something went really bad"
	}
	return json.Marshal(jsonErr)
}

type jsonError struct {
	Code    root.Code `json:"code"`
	Message string `json:"message"`
}
