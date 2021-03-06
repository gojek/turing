package testutils

import (
	"bytes"
	"io/ioutil"
	"net/http"

	mchttp "github.com/gojek/turing/engines/router/missionctl/http"
)

// MakeTestMisisonControlResponse makes a success response with a dummy json body for testing
func MakeTestMisisonControlResponse() mchttp.Response {
	httpResponse := &http.Response{
		StatusCode: http.StatusOK,
		Body:       ioutil.NopCloser(bytes.NewBuffer([]byte(`{"data": "test"}`))),
		Header:     http.Header{},
	}
	mcResp, _ := mchttp.NewCachedResponseFromHTTP(httpResponse)
	return mcResp
}
