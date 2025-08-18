# HTTP Consumer

A flexible HTTP consumer with support for multiple results, rate limiting, and controlled concurrency.

## Features

- **Rate Limiting**: Control the frequency of HTTP requests
- **Multiple Results**: Process multiple results from a single HTTP response
- **Worker Pools**: Separate workers for HTTP requests and result processing
- **Custom Parsers**: Define custom functions to parse response bodies
- **Concurrency Control**: Manage both HTTP request concurrency and result processing concurrency

## Configuration

```go
type HttpConf struct {
	Host           string        // Target host
	Path           string        // API path
	Token          string        // Bearer token
	Id             string        // Scanner ID
	Concurrency    int           // Number of HTTP request workers
	TimeOutSeconds int32         // HTTP timeout
	SleepTime      int32         // Sleep time on 404 (milliseconds)
	MaxResults     int           // Max results per request
	RateLimit      time.Duration // Minimum time between HTTP requests
	WorkerPoolSize int           // Number of result processing workers
	ParserFn       ParserFn      // Custom parser function
}
```

## Usage Examples

### Basic Usage

```go
conf := &consumer.HttpConf{
	Host:        "api.example.com",
	Path:        "/data",
	Token:       "your-token",
	Concurrency: 2,
	RateLimit:   2 * time.Second,
	WorkerPoolSize: 3,
}

consumer, err := consumer.NewHTTPConsumer(conf)
if err != nil {
	log.Fatal(err)
}

err = consumer.Start(context.Background(), func(data []byte) error {
	// Process each result
	fmt.Printf("Processing: %s\n", string(data))
	return nil
})
```

### With Custom Parser

```go
// Custom parser for JSON array responses
func jsonArrayParser(body []byte) ([]consumer.Result, error) {
	var data []interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	
	var results []consumer.Result
	for _, item := range data {
		itemBytes, _ := json.Marshal(item)
		results = append(results, consumer.Result{Data: itemBytes})
	}
	
	return results, nil
}

conf := &consumer.HttpConf{
	Host:        "api.example.com",
	Path:        "/data",
	Concurrency: 1,
	RateLimit:   1 * time.Second,
	WorkerPoolSize: 5,
}

consumer, err := consumer.NewHTTPConsumerWithParser(conf, jsonArrayParser)
if err != nil {
	log.Fatal(err)
}

err = consumer.Start(context.Background(), func(data []byte) error {
	// Process each individual result from the JSON array
	fmt.Printf("Processing item: %s\n", string(data))
	return nil
})
```

### Rate Limiting and Concurrency

The consumer provides two levels of concurrency control:

1. **HTTP Request Workers** (`Concurrency`): Control how many HTTP requests can be made simultaneously
2. **Result Processing Workers** (`WorkerPoolSize`): Control how many results can be processed simultaneously

The `RateLimit` ensures a minimum time between HTTP requests, preventing overwhelming the server.

## Architecture

```
HTTP Request Workers (Concurrency) 
    ↓ (Rate Limited)
HTTP Client
    ↓
Response Body
    ↓
Custom Parser (ParserFn)
    ↓
Multiple Results
    ↓
Result Channel (Buffered)
    ↓
Result Processing Workers (WorkerPoolSize)
    ↓
consumeFn(data)
```

## Benefits

1. **Efficient Resource Usage**: Rate limiting prevents overwhelming the server
2. **Flexible Processing**: Custom parsers allow handling various response formats
3. **Controlled Concurrency**: Separate control over HTTP requests and result processing
4. **Backpressure Handling**: Buffered channels prevent memory issues
5. **Error Handling**: Proper error propagation through the pipeline