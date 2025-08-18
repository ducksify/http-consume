package consumer

import (
	"errors"
	"net/http"
)

const (
	DefaultTimeOutSeconds = int32(5)
	DefaultConcurrency    = 1
	DefaultMaxResults     = 10
	DefaultRateLimit      = "3s"
	DefaultWorkerPoolSize = 5
	DefaultSleepTime404   = "30s"
)

var (
	SentinelErrorHostNotSet  = errors.New("queue not set")
	SentinelErrorPathNotSet  = errors.New("path not set")
	SentinelErrorConfigIsNil = errors.New("configuration is nil")
	SentinelHttpError        = errors.New("http call error")
	SentinelApplicationError = errors.New("http call error")
)

type HttpConf struct {
	Host                 string
	Path                 string
	Token                string
	Id                   string
	Concurrency          int
	TimeOutSeconds       int32
	SleepTime404         string // Sleep time on 404 (e.g., "1m", "1h30m")
	MaxResults           int    // Maximum number of results to process per request
	RateLimit            string // Minimum time between HTTP requests (e.g., "1s", "500ms")
	WorkerPoolSize       int    // Number of workers to process results
	SequentialProcessing bool   // If true, process items one at a time even with MaxResults > 1
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

// ParserFn is a function that parses HTTP response body into multiple results
type ParserFn func(body []byte) ([]Result, error)

// Result represents a single result from the HTTP response
type Result struct {
	Data []byte
	Err  error
}
