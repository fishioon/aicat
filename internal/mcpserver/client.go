package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Call(ctx context.Context, home, address, tool string, args json.RawMessage) (json.RawMessage, error) {
	token, err := os.ReadFile(filepath.Join(home, "mcp-token"))
	if err != nil {
		return nil, errors.New("MCP 凭据不可读")
	}
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "aicat", "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/mcp", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", "2025-06-18")
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 125 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("MCP 工具调用未能取得回执")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP 工具调用返回 HTTP %d", response.StatusCode)
	}
	var result struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError    bool            `json:"isError"`
			Structured json.RawMessage `json:"structuredContent"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil || len(result.Error) > 0 || result.Result.IsError {
		return nil, errors.New("MCP 工具请求被拒绝或响应无效")
	}
	payload := result.Result.Structured
	if len(payload) == 0 && len(result.Result.Content) > 0 {
		payload = []byte(result.Result.Content[0].Text)
	}
	return payload, nil
}
