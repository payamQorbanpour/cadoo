// cadoo-tunnel is the reverse-tunnel agent that lets Cadoo SaaS reach
// private GHES / GitLab installs without inbound firewall rules.
//
// Phase 3 ships the on-prem agent half of the protocol:
//
//   1. Agent dials the SaaS over HTTPS (outbound only).
//   2. Sends a hello frame with tenant ID + agent token.
//   3. Long-polls for forwarded webhook deliveries.
//   4. POSTs each delivery to the local cadoo-webhook (which already does
//      signature verification end-to-end since the body and headers are
//      forwarded verbatim).
//
// The SaaS-side endpoint that fans out webhook deliveries to connected
// agents is Phase 3.x — for now this binary establishes the connection,
// proves auth, and reports "no deliveries pending" on each long-poll.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type config struct {
	UpstreamURL  string
	TenantID     string
	AgentToken   string
	LocalForward string
	PollInterval time.Duration
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.UpstreamURL, "upstream", envOr("CADOO_TUNNEL_UPSTREAM", "https://api.cadoo.dev"),
		"Cadoo SaaS endpoint (the side that fans out webhook deliveries)")
	flag.StringVar(&cfg.TenantID, "tenant", os.Getenv("CADOO_TUNNEL_TENANT"), "tenant ID")
	flag.StringVar(&cfg.AgentToken, "token", os.Getenv("CADOO_TUNNEL_TOKEN"), "agent auth token")
	flag.StringVar(&cfg.LocalForward, "forward", envOr("CADOO_TUNNEL_FORWARD", "http://localhost:8081"),
		"local cadoo-webhook URL to POST forwarded deliveries to")
	flag.DurationVar(&cfg.PollInterval, "poll", 5*time.Second, "long-poll interval between checks")
	flag.Parse()

	if cfg.TenantID == "" || cfg.AgentToken == "" {
		fmt.Fprintln(os.Stderr, "cadoo-tunnel: --tenant and --token are required (or CADOO_TUNNEL_TENANT / CADOO_TUNNEL_TOKEN)")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := &agent{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second}}
	if err := a.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("tunnel exited", "err", err)
		os.Exit(1)
	}
}

type agent struct {
	cfg  config
	http *http.Client
}

// delivery is one forwarded webhook the SaaS asks the agent to replay locally.
type delivery struct {
	ID      string            `json:"id"`
	Headers map[string]string `json:"headers"`
	Path    string            `json:"path"`
	Body    []byte            `json:"body"`
}

type pollResponse struct {
	Deliveries []delivery `json:"deliveries"`
}

func (a *agent) run(ctx context.Context) error {
	slog.Info("tunnel starting",
		"upstream", a.cfg.UpstreamURL,
		"tenant", a.cfg.TenantID,
		"forward", a.cfg.LocalForward)

	backoff := time.Second
	for {
		err := a.pollOnce(ctx)
		if err == nil {
			backoff = time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(a.cfg.PollInterval):
			}
			continue
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		slog.Warn("poll failed; backing off", "err", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}

func (a *agent) pollOnce(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.cfg.UpstreamURL+"/v1/tunnel/poll?tenant="+a.cfg.TenantID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.AgentToken)
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("poll: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("poll status %d: %s", resp.StatusCode, string(body))
	}
	var pr pollResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return fmt.Errorf("decode poll response: %w", err)
	}
	for _, d := range pr.Deliveries {
		if err := a.forward(ctx, d); err != nil {
			slog.Error("forward delivery", "id", d.ID, "err", err)
			continue
		}
		slog.Info("forwarded delivery", "id", d.ID, "path", d.Path)
	}
	return nil
}

func (a *agent) forward(ctx context.Context, d delivery) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.cfg.LocalForward+d.Path, bytes.NewReader(d.Body))
	if err != nil {
		return err
	}
	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("local forward returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
