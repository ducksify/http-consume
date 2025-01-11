# Http Consume

this consumer get messages from a http endpoint and process them concurrently

### Envvar
Asume these variables are set
- HTTP_TOKEN

### Example
```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/panop-io/http-consume/consumer"
)

func test(b []byte) error {
	fmt.Println(string(b))
	time.Sleep(time.Second * 30)
	return nil
}

func main() {
	httpConf := consumer.HttpConf{
		Host: "tower.panop.io",
		Path: "/foo/bar",
		Token: "xyz",
	}
	c, err := consumer.NewHTTPConsumer(&httpConf)
	if err != nil {
		os.Exit(1)
	}
	c.Start(context.Background(), test)
}
```