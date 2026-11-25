// Command mcp bridges standard MCP stdio JSON-RPC to the MiniVerse GM /gm/mcp endpoint.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type bridge struct {
	baseURL string
	token   string
	client  *http.Client
}

func resolveToken(ctx context.Context, baseURL string, client *http.Client, lookup func(string) string) (string, error) {
	if tok := strings.TrimSpace(lookup("MINIVERSE_GM_TOKEN")); tok != "" {
		return tok, nil
	}
	user := strings.TrimSpace(lookup("MINIVERSE_GM_USER"))
	pass := lookup("MINIVERSE_GM_PASSWORD")
	if user == "" || pass == "" {
		return "", errors.New("set MINIVERSE_GM_TOKEN or MINIVERSE_GM_USER and MINIVERSE_GM_PASSWORD")
	}
	payload, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/gm/login", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gm login failed (%d): %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", errors.New("invalid login response")
	}
	return out.AccessToken, nil
}

func (b *bridge) forward(ctx context.Context, line []byte) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.baseURL, "/")+"/gm/mcp", bytes.NewReader(line))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.token)
	res, err := b.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusAccepted || res.StatusCode == http.StatusNoContent {
		return nil, false, nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, false, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, false, fmt.Errorf("mcp endpoint status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return bytes.TrimSpace(body), len(bytes.TrimSpace(body)) > 0, nil
}

func serveStdio(ctx context.Context, b *bridge, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 2<<20)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rawID struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.Unmarshal([]byte(line), &rawID)

		callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		resp, hasResp, err := b.forward(callCtx, []byte(line))
		cancel()
		if err != nil {
			if len(rawID.ID) == 0 || string(rawID.ID) == "null" {
				fmt.Fprintln(os.Stderr, "mcp notification error:", err)
				continue
			}
			rpcErr, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      rawID.ID,
				"error": map[string]any{
					"code":    -32000,
					"message": err.Error(),
				},
			})
			if _, wErr := fmt.Fprintf(w, "%s\n", rpcErr); wErr != nil {
				return wErr
			}
			continue
		}
		if hasResp {
			if _, wErr := fmt.Fprintf(w, "%s\n", resp); wErr != nil {
				return wErr
			}
		}
	}
	return scanner.Err()
}

func main() {
	baseURL := strings.TrimSpace(os.Getenv("MINIVERSE_API_URL"))
	if baseURL == "" {
		baseURL = "http://127.0.0.1:18080"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &http.Client{Timeout: 50 * time.Second}
	token, err := resolveToken(ctx, baseURL, client, os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "miniverse-mcp:", err)
		os.Exit(1)
	}
	b := &bridge{baseURL: baseURL, token: token, client: client}
	if err := serveStdio(ctx, b, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "miniverse-mcp:", err)
		os.Exit(1)
	}
}
