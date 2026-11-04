package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

type person struct {
	Token string `json:"token"`
}

type fixtures struct {
	Customers []person `json:"customers"`
	Providers []person `json:"providers"`
}

type recorder struct {
	mu        sync.Mutex
	latencies map[string][]time.Duration
	opened    int
	failed    int
}

func (r *recorder) observe(event string, id uuid.UUID) {
	if id.Version() != 7 {
		return
	}
	seconds, nanos := id.Time().UnixTime()
	latency := time.Since(time.Unix(seconds, nanos))
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latencies[event] = append(r.latencies[event], latency)
}

type summary struct {
	Count int     `json:"count"`
	P50   float64 `json:"p50_ms"`
	P95   float64 `json:"p95_ms"`
	P99   float64 `json:"p99_ms"`
	Max   float64 `json:"max_ms"`
}

func main() {
	base := flag.String("base", "", "API base URL")
	path := flag.String("fixtures", "", "fixtures file")
	idle := flag.Int("idle", 0, "customer streams to keep open")
	flag.Parse()

	raw, err := os.ReadFile(*path)
	if err != nil {
		log.Fatal(err)
	}
	var people fixtures
	if err := json.Unmarshal(raw, &people); err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	rec := &recorder{latencies: map[string][]time.Duration{}}

	client := &http.Client{Transport: &http.Transport{MaxIdleConns: 0, MaxConnsPerHost: 0, ForceAttemptHTTP2: false}}
	var wg sync.WaitGroup
	for _, provider := range people.Providers {
		wg.Go(func() { rec.listen(ctx, client, *base, provider.Token, "agora-provider-app") })
	}
	for i, customer := range people.Customers {
		if i == *idle {
			break
		}
		wg.Go(func() { rec.listen(ctx, client, *base, customer.Token, "agora-customer-app") })
	}
	wg.Wait()
	rec.report()
}

func (r *recorder) listen(ctx context.Context, client *http.Client, base, token, clientID string) {
	lastID := ""
	opened := false
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/events", http.NoBody)
		if err != nil {
			return
		}
		req.Header.Set("X-Client-Id", clientID)
		req.Header.Set("Authorization", "Bearer "+token)
		if lastID != "" {
			req.Header.Set("Last-Event-ID", lastID)
		}
		res, err := client.Do(req)
		if err != nil || res.StatusCode != http.StatusOK {
			r.mu.Lock()
			r.failed++
			r.mu.Unlock()
			if err == nil {
				_ = res.Body.Close()
			}
			time.Sleep(time.Second)
			continue
		}
		if !opened {
			opened = true
			r.mu.Lock()
			r.opened++
			r.mu.Unlock()
		}
		lastID = r.read(res, lastID)
		_ = res.Body.Close()
	}
}

func (r *recorder) read(res *http.Response, lastID string) string {
	scanner := bufio.NewScanner(res.Body)
	eventType := ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			lastID = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var body struct {
				EventID string `json:"event_id"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &body); err == nil {
				if id, err := uuid.Parse(body.EventID); err == nil {
					r.observe(eventType, id)
				}
			}
		case line == "":
			eventType = ""
		}
	}
	return lastID
}

func (r *recorder) report() {
	output := map[string]any{"streams_opened": r.opened, "stream_errors": r.failed}
	events := map[string]summary{}
	for event, latencies := range r.latencies {
		slices.Sort(latencies)
		at := func(q float64) float64 {
			return float64(latencies[int(q*float64(len(latencies)-1))].Microseconds()) / 1000
		}
		events[event] = summary{Count: len(latencies), P50: at(0.5), P95: at(0.95), P99: at(0.99), Max: at(1)}
	}
	output["created_to_delivered"] = events
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := os.Stdout.Write(append(encoded, '\n')); err != nil {
		log.Fatal(err)
	}
}
