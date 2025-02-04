package consumer

import (
	"errors"
	"net/http"
)

const (
	DefaultTimeOutSeconds = int32(5)
	DefaultConcurrency    = 1
)

var (
	SentinelErrorHostNotSet  = errors.New("queue not set")
	SentinelErrorPathNotSet  = errors.New("path not set")
	SentinelErrorConfigIsNil = errors.New("configuration is nil")
	SentinelHttpError        = errors.New("http call error")
	SentinelApplicationError = errors.New("http call error")
)

type HttpConf struct {
	Host           string
	Path           string
	Token          string
	Id             string
	Concurrency    int
	TimeOutSeconds int32
	SleepTime      int32
}

type HttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type HTTP struct {
	config     *HttpConf
	httpClient HttpClient
	httpReq    *http.Request
}

type ConsumerFn func(data []byte) error
