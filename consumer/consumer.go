package consumer

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sync/errgroup"
)

func NewHTTPConsumer(conf *HttpConf) (*HTTP, error) {

	// check if config is nil and return error
	if conf == nil {
		return nil, SentinelErrorConfigIsNil
	}

	// check if time out seconds is set and set default if not
	if conf.TimeOutSeconds == 0 {
		conf.TimeOutSeconds = DefaultTimeOutSeconds
	}

	// check if concurrency is set and set default if not
	if conf.Concurrency == 0 {
		conf.Concurrency = DefaultConcurrency
	}

	// check if host is set and return error if not
	if conf.Host == "" {
		return nil, SentinelErrorHostNotSet
	}

	// check if path is set and return error if not
	if conf.Path == "" {
		return nil, SentinelErrorPathNotSet
	}

	// check if sleep time is set and set default if not
	if conf.SleepTime404 == "" {
		conf.SleepTime404 = DefaultSleepTime404
	}

	// check if max results is set and set default if not
	if conf.MaxResults == 0 {
		conf.MaxResults = DefaultMaxResults
	}

	// check if rate limit is set and set default if not
	if conf.RateLimit == "" {
		conf.RateLimit = DefaultRateLimit
	}

	// create http client
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{Transport: tr, Timeout: time.Second * 3}

	urlHttp := url.URL{
		Scheme: "https",
		Host:   conf.Host,
		Path:   fmt.Sprintf("%s/%d", conf.Path, conf.MaxResults),
	}

	httpRequest, _ := http.NewRequest(http.MethodGet, urlHttp.String(), nil)
	// inject Bearer if token is set
	if conf.Token != "" {
		httpRequest.Header.Set("Authorization", fmt.Sprintf("Bearer %s", conf.Token))
	}
	// inject Scanner ID if set
	if conf.Id != "" {
		httpRequest.Header.Set("X-Panop-Scanner", conf.Id)
	}

	return &HTTP{config: conf, httpClient: httpClient, httpReq: httpRequest}, nil
}

func (s *HTTP) Start(ctx context.Context, consumeFn ConsumerFn) error {
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		cancel()
	}()

	g, ctx := errgroup.WithContext(ctx)

	// Create a rate limiter for HTTP requests
	rateLimitDuration, err := time.ParseDuration(s.config.RateLimit)
	if err != nil {
		return fmt.Errorf("error parsing rate limit: %w", err)
	}
	rateLimiter := time.NewTicker(rateLimitDuration)
	defer rateLimiter.Stop()

	// Create a single channel for parsed results and is sized to WorkerPoolSize
	processChan := make(chan Result, s.config.Concurrency)

	// Start worker pool to process results
	for i := 0; i < s.config.Concurrency; i++ {
		g.Go(func() error {
			return s.resultWorker(ctx, processChan, consumeFn)
		})
	}

	// Start HTTP request workers with rate limiting
	for i := 0; i < s.config.Concurrency; i++ {
		g.Go(func() error {
			return s.handleMessagesWithRateLimit(ctx, processChan, rateLimiter.C)
		})
	}

	return g.Wait()
}

func (s *HTTP) handleMessagesWithRateLimit(ctx context.Context, processChan chan Result, rateLimiter <-chan time.Time) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-rateLimiter:
			select {
			case <-ctx.Done():
				return nil
			default:
				// Issue request; downstream backpressure is enforced by blocking sends to processChan

				result, err := s.httpClient.Do(s.httpReq)
				if err != nil {
					return fmt.Errorf("error during call: %w,  %w", err, SentinelApplicationError)
				}

				switch result.StatusCode {
				case http.StatusOK:
					// return error if no body
					if result.Body == nil {
						return fmt.Errorf("error body is nil: %w", SentinelApplicationError)
					}
					body, err := io.ReadAll(result.Body)
					result.Body.Close()
					if err != nil {
						return fmt.Errorf("error reading body: %w,  %w", err, SentinelApplicationError)
					}

					// parse and send results directly; blocking send applies backpressure
					for _, res := range s.parseMultipleResults(body) {
						select {
						case processChan <- res:
						case <-ctx.Done():
							return nil
						}
					}
				case http.StatusNotFound:
					if err := s.sleepOnError(ctx); err != nil {
						return err
					}
				// other http status
				default:
					slog.Error("error http during call", "status", result.StatusCode, "error", SentinelHttpError)
					if err := s.sleepOnError(ctx); err != nil {
						return err
					}
				}
			}
		}
	}
}

// parseMultipleResults parses the response body into multiple results
// This is a simple implementation - you can customize this based on your API response format
func (s *HTTP) parseMultipleResults(body []byte) []Result {
	var results []Result

	// parse scans from JSON response
	var response struct {
		Scans []json.RawMessage `json:"scans"`
	}

	if err := json.Unmarshal(body, &response); err == nil && len(response.Scans) > 0 {
		// Parse scans array
		for _, scan := range response.Scans {
			results = append(results, Result{Data: scan})
		}
	} else {
		// Fallback: treat the entire body as one result
		results = append(results, Result{Data: body})
	}

	return results
}

func (s *HTTP) resultWorker(ctx context.Context, resultChan chan Result, consumeFn ConsumerFn) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case result := <-resultChan:
			// Handle error results
			if result.Err != nil {
				return fmt.Errorf("parser error: %w", result.Err)
			}

			if err := consumeFn(result.Data); err != nil {
				return err
			}
		}
	}
}

// parserWorker removed: HTTP workers parse and enqueue results directly

// sleepOnError handles sleeping on errors with context cancellation support
func (s *HTTP) sleepOnError(ctx context.Context) error {
	sleepTime, err := time.ParseDuration(s.config.SleepTime404)
	if err != nil {
		return fmt.Errorf("error parsing sleep time: %w", err)
	}
	select {
	case <-time.After(sleepTime):
		return nil
	case <-ctx.Done():
		fmt.Println("interrupted immediately.")
		return nil
	}
}
