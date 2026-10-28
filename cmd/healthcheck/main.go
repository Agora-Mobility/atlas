package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	defaultPort  = 8080
	probeTimeout = 2 * time.Second
)

func main() {
	os.Exit(probe())
}

func probe() int {
	port, ok := httpPort()
	if !ok {
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%d/live", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody) // #nosec G704 -- fixed loopback host, numeric port
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req) // #nosec G704 -- fixed loopback host, numeric port
	if err != nil {
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func httpPort() (int, bool) {
	value := os.Getenv("HTTP_PORT")
	if value == "" {
		return defaultPort, true
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}
