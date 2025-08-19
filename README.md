### HTTP Consumer

A flexible HTTP consumer with support for multiple results per response, string-based rate limiting, and two-stage concurrency.

### Features

- **Rate limiting (string durations)**: Control request pace with values like "500ms", "1s", "2m".
- **Multiple results per response**: Each HTTP response can produce up to `MaxResults` items.
- **Single-stage concurrency**: Control request and processing concurrency with `Concurrency`.
- **Backpressure**: Blocking channel sends ensure downstream saturation naturally throttles upstream.
- **Graceful shutdown and error propagation**.

### Configuration

```go
type HttpConf struct {
    Host                 string        // Target host
    Path                 string        // API path
    Token                string        // Bearer token
    Id                   string        // Scanner ID
    Concurrency          int           // Number of HTTP request workers
    TimeOutSeconds       int32         // HTTP timeout (seconds)
    SleepTime404         string        // Sleep time on 404 (e.g., "30s", "1m30s")
    MaxResults           int           // Max results requested per call (API specific)
    RateLimit            string        // Minimum time between HTTP requests (e.g., "1s", "500ms")
    
    SequentialProcessing bool          // If true, adds a tiny delay between items
}
```

Notes:

- `RateLimit` is parsed with `time.ParseDuration`.

### Basic Usage

```go
conf := &consumer.HttpConf{
    Host:           "api.example.com",
    Path:           "/data",
    Token:          "your-token",
    Concurrency:    2,
    RateLimit:      "2s",
    
    SleepTime404:   "30s",
}

c, err := consumer.NewHTTPConsumer(conf)
if err != nil {
    log.Fatal(err)
}

err = c.Start(context.Background(), func(data []byte) error {
    // Process each result
    fmt.Printf("Processing: %s\n", string(data))
    return nil
})
if err != nil {
    log.Fatal(err)
}
```

### Rate Limiting and Concurrency

The consumer provides two levels of concurrency control:

1. **HTTP Request Workers** (`Concurrency`): how many HTTP workers run in parallel (each obeys `RateLimit`).
2. **Result Processing**: Results are processed by the same workers that make HTTP requests.

`RateLimit` ensures a minimum gap between requests from each worker.

### Architecture

```
HTTP Request Workers (Concurrency)
    ↓ (Rate Limited)
HTTP Client
    ↓
Response Body
    ↓
Parse into multiple results (internal)
    ↓
Result Channel (buffered by Concurrency)
    ↓
Result Processing (same workers)
    ↓
consumeFn(data)
```

### Benefits

- **Efficient resource usage**: Rate limiting prevents overwhelming the server.
- **Controlled concurrency**: Single control over HTTP requests and result processing.
- **Backpressure handling**: Blocking sends on the result channel prevent overload.
- **Error handling**: Proper error propagation through the pipeline.