package market

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	marketHTTPAttempts = 3
	marketHTTPBackoff  = 300 * time.Millisecond
)

func doMarketRequestBody(client *http.Client, req *http.Request) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	var lastErr error
	for attempt := 1; attempt <= marketHTTPAttempts; attempt++ {
		resp, err := client.Do(req.Clone(req.Context()))
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if resp.StatusCode == http.StatusOK {
				return body, nil
			} else {
				lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
			}
		}

		if attempt < marketHTTPAttempts {
			time.Sleep(time.Duration(attempt) * marketHTTPBackoff)
		}
	}

	return nil, lastErr
}
