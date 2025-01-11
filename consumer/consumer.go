package consumer

import (
	"context"
	"crypto/tls"
	"fmt"
	"golang.org/x/sync/errgroup"
	"io"
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
	httpRequest.Header.Set("Authorization", "Bearer "+conf.Token)

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

			if result.StatusCode != http.StatusOK {
				return fmt.Errorf("error http during call: %s,  %w", http.StatusText(result.StatusCode),
					SentinelHttpError)
			}
			body, err := io.ReadAll(result.Body)
			result.Body.Close()
			if err != nil {
				return fmt.Errorf("error reading body: %w,  %w", err, SentinelApplicationError)
			}

			if err := consumeFn(body); err != nil {
				return err
			}

		}
	}
}
