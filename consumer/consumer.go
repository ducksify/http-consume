package consumer

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"
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

	// check if sleep time for call errors is set and set default if not
	if conf.SleepTimeCallError == "" {
		conf.SleepTimeCallError = DefaultSleepTimeCallError
	}

	// check if max backoff is set and set default if not
	if conf.MaxBackoff == "" {
		conf.MaxBackoff = DefaultMaxBackoff
	}

	// check if max results is set and set default if not
	if conf.MaxResults == 0 {
		conf.MaxResults = DefaultMaxResults
	}

	// concurrency should be greater than max results
	if conf.Concurrency < conf.MaxResults {
		return nil, SentinelErrorConcurrencyLessThanMaxResults
	}

	// check if rate limit is set and set default if not

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

	return &HTTP{config: conf,
		httpClient: httpClient,
		httpReq:    httpRequest,
		semaphore:  make(chan struct{}, conf.Concurrency)}, nil
}

func (s *HTTP) Start(ctx context.Context, consumeFn ConsumerFn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Graceful shutdown
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil

		default:
			// Only poll SQS when we have semaphore capacity
			availableSlots := cap(s.semaphore) - len(s.semaphore)
			if availableSlots >= s.config.MaxResults {
				slog.Info("Polling messages from Tower, available slots : ", slog.Int("availableSlots", availableSlots))
				result, err := s.httpClient.Do(s.httpReq)
				if err != nil {
					slog.Error("error during call", "error", err, "sentinel", SentinelApplicationError)
					if err := s.sleepOnError(ctx); err != nil {
						return err
					}
					continue
				}
				defer result.Body.Close()

				if result.Body == nil {
					slog.Error("error body is nil", "sentinel", SentinelApplicationError)
					if err := s.sleepOnError(ctx); err != nil {
						return err
					}
					continue
				}

				body, err := io.ReadAll(result.Body)
				if err != nil {
					slog.Error("error reading body", "error", err)
					if err := s.sleepOnError(ctx); err != nil {
						return err
					}
					continue
				}

				// Handle different HTTP status codes
				switch result.StatusCode {
				case http.StatusOK:
					// Reset error count on successful response
					s.resetErrorCount()
					// Process the successful response
				case http.StatusNotFound:
					slog.Debug("No messages available, sleeping")
					if err := s.sleepOn404(ctx); err != nil {
						return err
					}
					continue
				default:
					slog.Error("error http during call", "status", result.StatusCode, "error", SentinelHttpError)
					if err := s.sleepOnError(ctx); err != nil {
						return err
					}
					continue
				}

				// Process messages with proper semaphore handling
				messages := s.parseMultipleResults(body)
				for _, msg := range messages {
					select {
					case s.semaphore <- struct{}{}:
						go func(m Result) {
							defer func() { <-s.semaphore }() // release the semaphore slot once done
							consumeFn(m.Data)
						}(msg)
					case <-ctx.Done():
						continue
					default:
						// Semaphore is full, wait for a slot to become available
						slog.Debug("Semaphore full, waiting for available slot")
						select {
						case s.semaphore <- struct{}{}:
							go func(m Result) {
								defer func() { <-s.semaphore }()
								consumeFn(m.Data)
							}(msg)
						case <-ctx.Done():
							return nil
						}
					}
				}
			} else {
				slog.Debug("semaphore is full, waiting for 500ms before checking again", slog.Int("availableSlots", availableSlots))
				select {
				case <-time.After(1000 * time.Millisecond):
				case <-ctx.Done():
					return nil
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

// calculateExponentialBackoff calculates sleep time with exponential backoff, jitter, and configurable max
func (s *HTTP) calculateExponentialBackoff(baseDuration time.Duration) time.Duration {
	if s.errorCount <= 0 {
		return baseDuration
	}

	// Parse max backoff from config
	maxBackoff, err := time.ParseDuration(s.config.MaxBackoff)
	if err != nil {
		// Fallback to 5 minutes if parsing fails
		maxBackoff = 5 * time.Minute
	}

	// Calculate exponential backoff: baseDuration * 2^(errorCount-1)
	// This gives us: 1x, 2x, 4x, 8x, 16x, etc.
	multiplier := math.Pow(2, float64(s.errorCount-1))
	backoffDuration := time.Duration(float64(baseDuration) * multiplier)

	// Cap the maximum backoff to prevent extremely long waits
	if backoffDuration > maxBackoff {
		backoffDuration = maxBackoff
	}

	// Add jitter (±25% random variation) to prevent thundering herd
	// Jitter helps distribute retry attempts across time
	jitterRange := float64(backoffDuration) * 0.25 // 25% of the duration
	jitter := time.Duration(rand.Float64()*jitterRange*2) - time.Duration(jitterRange)

	finalBackoff := backoffDuration + jitter

	// Ensure we don't go below the base duration
	if finalBackoff < baseDuration {
		finalBackoff = baseDuration
	}

	// Ensure we don't exceed the max backoff (including jitter)
	if finalBackoff > maxBackoff {
		finalBackoff = maxBackoff
	}

	return finalBackoff
}

// sleepOnError handles sleeping on errors with context cancellation support and exponential backoff
func (s *HTTP) sleepOnError(ctx context.Context) error {
	// Increment error count for exponential backoff
	s.errorCount++

	// Use SleepTimeCallError for call errors (non-404 errors)
	sleepTime, err := time.ParseDuration(s.config.SleepTimeCallError)
	if err != nil {
		return fmt.Errorf("error parsing sleep time: %w", err)
	}

	// Apply exponential backoff
	backoffDuration := s.calculateExponentialBackoff(sleepTime)

	select {
	case <-time.After(backoffDuration):
		slog.Debug("sleeping on error", "backoffDuration", backoffDuration)
		return nil
	case <-ctx.Done():
		fmt.Println("interrupted immediately.")
		return nil
	}
}

// resetErrorCount resets the error count to 0
func (s *HTTP) resetErrorCount() {
	s.errorCount = 0
}

// sleepOn404 handles sleeping on 404 errors (no backoff, uses SleepTime404)
func (s *HTTP) sleepOn404(ctx context.Context) error {
	// Reset error count on successful 404 (no actual error, just no data)
	s.resetErrorCount()

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
