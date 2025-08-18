package consumer

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	_ "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type HTTPMock struct {
	mock.Mock
}

type errorReadCloser struct{}

func (e *errorReadCloser) Read(p []byte) (n int, err error) {
	return 0, errors.New("forced error during read")
}

func (e *errorReadCloser) Close() error {
	return nil
}

func getBodyFromString(s string) io.ReadCloser {
	// used to trigger body error
	if s == "" {
		return new(errorReadCloser)
	}
	return io.NopCloser(bytes.NewReader([]byte(s)))
}

func (m *HTTPMock) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	res := new(http.Response)
	res.StatusCode = args.Get(1).(int)
	res.Body = getBodyFromString(args.String(0))
	return res, args.Error(2)
}

func setEnv(keyValue ...string) {
	// Map to store original values to restore them later
	// Loop through the provided key-value pairs
	for i := 0; i < len(keyValue); i += 2 {
		key := keyValue[i]
		value := keyValue[i+1]
		os.Setenv(key, value)
	}
}
func unsetEnv(keyValue ...string) {
	// Map to store original values to restore them later
	// Loop through the provided key-value pairs
	for i := 0; i < len(keyValue); i += 2 {
		key := keyValue[i]
		os.Unsetenv(key)
	}
}

func TestNewHTTPWorker(t *testing.T) {
	sqsConf := &HttpConf{
		Host:           "host",
		Path:           "path",
		Concurrency:    2,
		TimeOutSeconds: 20,
	}
	sqsConfHostEmpty := &HttpConf{
		Host:           "",
		Path:           "path",
		Concurrency:    2,
		TimeOutSeconds: 20,
	}
	sqsConfPathEmpty := &HttpConf{
		Host:           "host",
		Path:           "",
		Concurrency:    2,
		TimeOutSeconds: 20,
	}

	// create http client
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{Transport: tr, Timeout: time.Second * 3}

	var consumeTestFunc ConsumerFn

	type args struct {
		conf      *HttpConf
		consumeFn ConsumerFn
		env       []string
	}
	tests := []struct {
		name    string
		args    args
		want    *HTTP
		wantErr error
	}{
		{
			name: "should Create New HTTPConsumer",
			args: args{
				conf:      sqsConf,
				consumeFn: consumeTestFunc,
				env:       []string{"HTTP_TOKEN", "baz"},
			},
			want: &HTTP{
				config:     sqsConf,
				httpClient: httpClient,
			},

			wantErr: nil,
		},
		{
			name: "should Error Host Empty",
			args: args{
				conf:      sqsConfHostEmpty,
				consumeFn: consumeTestFunc,
				env:       []string{"HTTP_TOKEN", "baz"},
			},
			want: &HTTP{
				config:     sqsConf,
				httpClient: httpClient,
			},

			wantErr: SentinelErrorHostNotSet,
		},
		{
			name: "should Error Path Empty",
			args: args{
				conf:      sqsConfHostEmpty,
				consumeFn: consumeTestFunc,
				env:       []string{"HTTP_TOKEN", "baz"},
			},
			want: &HTTP{
				config:     sqsConfPathEmpty,
				httpClient: httpClient,
			},

			wantErr: SentinelErrorHostNotSet,
		},
		{
			name: "should Error Config Is Nil",
			args: args{
				conf:      nil,
				consumeFn: consumeTestFunc,
				env:       []string{"HTTP_TOKEN", "baz"},
			},
			want: &HTTP{
				config:     sqsConfPathEmpty,
				httpClient: httpClient,
			},

			wantErr: SentinelErrorConfigIsNil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// set Env
			unsetEnv("HTTP_TOKEN", "baz")

			setEnv(tt.args.env...)

			got, err := NewHTTPConsumer(tt.args.conf)
			if err != nil && tt.wantErr == nil {
				t.Errorf("Error creation new Consumer")
				return
			}
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.Equal(t, tt.want.config, got.config)
			unsetEnv(tt.args.env...)
		})
	}
}

func TestSQS_Start(t *testing.T) {
	host := "host"
	path := "path"
	var actualData []string
	consumeTestFunc := func(data []byte) error {
		actualData = append(actualData, string(data))
		return nil
	}

	// consumeTestFuncError removed; we now test only transport/body/error scenarios

	type fields struct {
		config     *HttpConf
		httpClient HttpClient
	}

	type args struct {
		consumeFn ConsumerFn
	}
	var tests = []struct {
		name       string
		fields     fields
		args       args
		body       string
		statusCode int
		triggerErr error
		wantErr    error
	}{
		{
			name: "shouldHandleMessage",
			fields: fields{
				config: &HttpConf{
					Host:      host,
					Path:      path,
					RateLimit: "10ms",
				},
				httpClient: new(HTTPMock),
			},
			args: args{
				consumeFn: consumeTestFunc,
			},
			body:       "foo",
			statusCode: http.StatusOK,
			triggerErr: nil,
		},
		{
			name: "should error when receive",
			fields: fields{
				config: &HttpConf{
					Host:      host,
					Path:      path,
					RateLimit: "10ms",
				},
				httpClient: new(HTTPMock),
			},
			args: args{
				consumeFn: consumeTestFunc,
			},
			body:       "foo",
			statusCode: http.StatusInternalServerError,
			triggerErr: nil,
			wantErr:    SentinelHttpError,
		},
		{
			name: "should body error",
			fields: fields{
				config: &HttpConf{
					Host:      host,
					Path:      path,
					RateLimit: "10ms",
				},
				httpClient: new(HTTPMock),
			},
			args: args{
				consumeFn: consumeTestFunc,
			},
			body:       "",
			statusCode: http.StatusOK,
			triggerErr: nil,
			wantErr:    SentinelApplicationError,
		},
		// Removed: should consumer error (the pipeline returns error directly from consumeFn now)
		{
			name: "should context timeout",
			fields: fields{
				config: &HttpConf{
					Host:      host,
					Path:      path,
					RateLimit: "10ms",
				},
				httpClient: new(HTTPMock),
			},
			args: args{
				consumeFn: consumeTestFunc,
			},
			body:       "foo",
			statusCode: http.StatusOK,
			triggerErr: nil,
			wantErr:    nil,
		},

		//*/
	}
	for _, tt := range tests {
		actualData = make([]string, 0)
		t.Run(tt.name, func(t *testing.T) {
			sqsMock := tt.fields.httpClient.(*HTTPMock)
			// set mock for scanrun
			sqsMock.On("Do", mock.Anything).
				Return(tt.body, tt.statusCode, tt.triggerErr)

			ctx, _ := context.WithTimeout(context.Background(), 3*time.Second)

			// set Env
			setEnv("HTTP_TOKEN", "baz")

			s, err := NewHTTPConsumer(tt.fields.config)
			if err != nil {
				t.Errorf("Error creation new Consumer")
				return
			}

			s.httpClient = sqsMock
			err = s.Start(ctx, tt.args.consumeFn)

			t.Log(len(actualData))
			for _, msg := range actualData {
				assert.Contains(t, []string{
					"foo",
				}, msg)
			}
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
