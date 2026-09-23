package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"easynotify/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func run() error {
	address := os.Getenv("EASYNOTIFY_URL")
	if address == "" {
		address = "http://127.0.0.1:8787"
	}
	base, err := url.Parse(address)
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return errors.New("EASYNOTIFY_URL must be an HTTP(S) origin, e.g. http://127.0.0.1:8787")
	}
	base.Path = "/notify"
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are not allowed") }}
	sdk := mcp.NewServer(&mcp.Implementation{Name: "easynotify", Version: "1.0.0"}, nil)
	mcp.AddTool(sdk, &mcp.Tool{
		Name: "notify", Description: "Queue a task completion or failure notification for EasyNotify. Messages remain in memory until a receiver acknowledges them; restarting the relay clears pending messages. Do not retry automatically: duplicate notifications may result.",
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"title", "description"},
			"properties": map[string]any{
				"title":       map[string]any{"type": "string", "minLength": 1, "maxLength": 30, "description": "Notification title, up to 30 Unicode characters."},
				"description": map[string]any{"type": "string", "minLength": 1, "maxLength": 500, "description": "Markdown description, up to 500 Unicode characters."},
			},
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input server.Input) (*mcp.CallToolResult, any, error) {
		if err := input.Validate(); err != nil {
			return nil, nil, err
		}
		body, _ := json.Marshal(input)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			return nil, nil, errors.New("cannot reach EasyNotify relay or response timed out. Delivery is unknown; check the client before retrying")
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 16385))
		if err != nil || len(data) > 16384 || !json.Valid(data) {
			return nil, nil, errors.New("invalid relay response; delivery is unknown")
		}
		if response.StatusCode != http.StatusOK {
			var failure struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(data, &failure)
			if failure.Error == "" {
				failure.Error = fmt.Sprintf("HTTP %d", response.StatusCode)
			}
			return nil, nil, errors.New(failure.Error)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimSpace(string(data))}}}, nil, nil
	})
	return sdk.Run(context.Background(), &mcp.StdioTransport{})
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
