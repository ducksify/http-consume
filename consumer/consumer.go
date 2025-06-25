package consumer

import (
	"context"
	"crypto/tls"
	"fmt"
	"golang.org/x/sync/errgroup"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"
)

func NewHTTPConsumer(conf *HttpConf) (*HTTP, error) {

	if conf == nil {
		return nil, SentinelErrorConfigIsNil
	}

	if conf.TimeOutSeconds == 0 {
		conf.TimeOutSeconds = DefaultTimeOutSeconds
	}

	if conf.Concurrency == 0 {
		conf.Concurrency = DefaultConcurrency
	}

	if conf.Host == "" {
		return nil, SentinelErrorHostNotSet
	}

	if conf.Path == "" {
		return nil, SentinelErrorPathNotSet
	}
	if conf.SleepTime == 0 {
		conf.SleepTime = 10000
	}

	// create http client
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{Transport: tr, Timeout: time.Second * 3}

	urlHttp := url.URL{
		Scheme: "https",
		Host:   conf.Host,
		Path:   conf.Path,
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
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt)
		_ = <-c
		cancel()
	}()

	g, ctx := errgroup.WithContext(ctx)

	for i := 0; i < s.config.Concurrency; i++ {
		time.Sleep(time.Duration(rand.Intn(5)+1) * time.Second)
		g.Go(func() error {
			return s.handleMessages(ctx, consumeFn)
		})
	}

	return g.Wait()
}

func (s *HTTP) handleMessages(ctx context.Context, consumeFn ConsumerFn) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
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

				if err := consumeFn(body); err != nil {
					return err
				}
				select {
				case <-time.After(time.Duration(1000) * time.Millisecond):
				case <-ctx.Done():
					return nil
				}

			// if not found sleep for a while
			case http.StatusNotFound:
				select {
				case <-time.After(time.Duration(s.config.SleepTime) * time.Millisecond):
					// continue
				case <-ctx.Done():
					fmt.Println("Sleep interrupted immediately by shutdown.")
				}
				// other http status
			default:
				return fmt.Errorf("error http during call: %s,  %w", http.StatusText(result.StatusCode), SentinelHttpError)
			}

		}
	}
}
