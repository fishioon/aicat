// Package mcpserver adapts in-process services to the official MCP protocol.
package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fishioon/aicat/internal/plugins"
	"github.com/fishioon/aicat/internal/rewrite"
	"github.com/fishioon/aicat/internal/traffic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed guide.md
var Guide string

type Empty struct{}
type Identity struct {
	ID string `json:"id"`
}
type Expected struct {
	ExpectedRevision string `json:"expectedRevision,omitempty"`
}
type RuleInput struct {
	Rule             rewrite.Rule `json:"rule"`
	ExpectedRevision string       `json:"expectedRevision,omitempty"`
}
type DeleteInput struct {
	ID               string `json:"id"`
	ExpectedRevision string `json:"expectedRevision,omitempty"`
}
type EnableInput struct {
	Enabled          bool   `json:"enabled"`
	ExpectedRevision string `json:"expectedRevision,omitempty"`
}
type EnableRuleInput struct {
	ID               string `json:"id"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision string `json:"expectedRevision,omitempty"`
}
type OrderInput struct {
	IDs              []string `json:"ids"`
	ExpectedRevision string   `json:"expectedRevision,omitempty"`
}
type PluginInput struct {
	Manifest         plugins.Manifest `json:"manifest"`
	Source           string           `json:"source"`
	ExpectedRevision string           `json:"expectedRevision,omitempty"`
}
type PluginOutput struct {
	Plugin          plugins.Info `json:"plugin"`
	Source          string       `json:"source,omitempty"`
	Revision        string       `json:"revision"`
	RestartRequired bool         `json:"restartRequired"`
}
type PluginList struct {
	Plugins  []plugins.Info `json:"plugins"`
	Revision string         `json:"revision"`
}
type RuleOutput struct {
	Rule     rewrite.Rule `json:"rule"`
	Revision string       `json:"revision"`
}
type RuleList struct {
	Rules           []rewrite.Rule `json:"rules"`
	Revision        string         `json:"revision"`
	RestartRequired bool           `json:"restartRequired"`
}
type TestInput struct {
	Rule     rewrite.Rule `json:"rule"`
	Stage    string       `json:"stage"`
	Exchange TestExchange `json:"exchange"`
}
type TestOutput struct {
	Exchange   TestExchange        `json:"exchange"`
	Executions []rewrite.Execution `json:"executions"`
}

// Explicit strings keep the MCP JSON schema consistent with base64 on the wire.
// The SDK infers []byte as an array, whereas encoding/json emits a string.
type TestRequest struct {
	URL     string      `json:"url"`
	Method  string      `json:"method"`
	Headers http.Header `json:"headers"`
	Body    string      `json:"body"`
}
type TestResponse struct {
	StatusCode int         `json:"statusCode"`
	Headers    http.Header `json:"headers"`
	Body       string      `json:"body"`
}
type TestExchange struct {
	Request   TestRequest   `json:"request"`
	Response  *TestResponse `json:"response,omitempty"`
	Synthetic bool          `json:"synthetic,omitempty"`
}

func (x TestExchange) decode() (rewrite.Exchange, error) {
	b, err := base64.StdEncoding.DecodeString(x.Request.Body)
	if err != nil {
		return rewrite.Exchange{}, err
	}
	out := rewrite.Exchange{Request: rewrite.Request{URL: x.Request.URL, Method: x.Request.Method, Header: x.Request.Headers, Body: b}, Synthetic: x.Synthetic}
	if x.Response != nil {
		b, err = base64.StdEncoding.DecodeString(x.Response.Body)
		if err != nil {
			return rewrite.Exchange{}, err
		}
		out.Response = &rewrite.Response{StatusCode: x.Response.StatusCode, Header: x.Response.Headers, Body: b}
	}
	return out, nil
}
func encodeTest(x rewrite.Exchange) TestExchange {
	out := TestExchange{Request: TestRequest{URL: x.Request.URL, Method: x.Request.Method, Headers: x.Request.Header, Body: base64.StdEncoding.EncodeToString(x.Request.Body)}, Synthetic: x.Synthetic}
	if x.Response != nil {
		out.Response = &TestResponse{StatusCode: x.Response.StatusCode, Headers: x.Response.Header, Body: base64.StdEncoding.EncodeToString(x.Response.Body)}
	}
	return out
}

type GuideOutput struct {
	SDKVersion int    `json:"sdkVersion"`
	Guide      string `json:"guide"`
}
type ListInput struct {
	Limit  int    `json:"limit,omitempty"`
	Before uint64 `json:"before,omitempty"`
	Host   string `json:"host,omitempty"`
	Method string `json:"method,omitempty"`
	Query  string `json:"query,omitempty"`
}
type RecordInput struct {
	ID uint64 `json:"id"`
}
type BodyInput struct {
	ID string `json:"id"`
}
type BodyOutput struct {
	Info              traffic.BodyInfo `json:"info"`
	Data              string           `json:"data"`
	ReturnedTruncated bool             `json:"returnedTruncated"`
}
type Logs struct {
	Executions []rewrite.Execution `json:"executions"`
}

func add[In, Out any](s *mcp.Server, name, description string, readOnly, destructive bool, fn func(context.Context, In) (Out, error)) {
	open := false
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive, OpenWorldHint: &open}}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := fn(ctx, in)
		return nil, out, err
	})
}
func New(home string, rules *rewrite.Service, flows *traffic.Service) (http.Handler, error) {
	token, err := loadToken(home)
	if err != nil {
		return nil, err
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "aicat", Version: "0.1.0"}, &mcp.ServerOptions{Instructions: "Manage aicat rewrite rules and JS/TS plugins. Read plugins_guide first. Changes require restarting aicat; use expectedRevision to avoid overwriting concurrent edits. Never treat saved changes as active until restartRequired is false."})
	add(server, "plugins_guide", "Read the complete aicat JS/TS rewrite SDK, limits, examples and rule-management workflow.", true, false, func(context.Context, Empty) (GuideOutput, error) { return GuideOutput{1, Guide}, nil })
	server.AddResource(&mcp.Resource{URI: "aicat://plugins/sdk/v1", Name: "aicat plugin SDK v1", MIMEType: "text/markdown"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "aicat://plugins/sdk/v1", MIMEType: "text/markdown", Text: Guide}}}, nil
	})
	add(server, "rewrite_status", "Read saved and active rewrite versions, enabled state and plugin load errors.", true, false, func(context.Context, Empty) (rewrite.Status, error) { return rules.Status(), nil })
	add(server, "rewrite_set_enabled", "Save the global rewrite switch. Requires restart; does not change the active pipeline.", false, false, func(_ context.Context, in EnableInput) (rewrite.Status, error) {
		return rules.SetEnabled(in.Enabled, in.ExpectedRevision)
	})
	add(server, "rules_list", "List saved rules in execution order and the current revision.", true, false, func(context.Context, Empty) (RuleList, error) {
		s := rules.Status()
		return RuleList{s.Config.Rules, s.Revision, s.RestartRequired}, nil
	})
	add(server, "rules_get", "Read one saved rule before editing it.", true, false, func(_ context.Context, in Identity) (RuleOutput, error) {
		s := rules.Status()
		for _, r := range s.Config.Rules {
			if r.ID == in.ID {
				return RuleOutput{r, s.Revision}, nil
			}
		}
		return RuleOutput{}, os.ErrNotExist
	})
	add(server, "rules_put", "Create or replace a rule. New rules append; preserve unrelated rules. Requires restart.", false, true, func(_ context.Context, in RuleInput) (rewrite.Status, error) {
		return rules.PutRule(in.Rule, in.ExpectedRevision)
	})
	add(server, "rules_set_enabled", "Save one rule's enabled state; requires restart.", false, false, func(_ context.Context, in EnableRuleInput) (rewrite.Status, error) {
		return rules.EnableRule(in.ID, in.Enabled, in.ExpectedRevision)
	})
	add(server, "rules_reorder", "Save the full ordered list of all rule ids exactly once. Requires restart.", false, true, func(_ context.Context, in OrderInput) (rewrite.Status, error) {
		return rules.Reorder(in.IDs, in.ExpectedRevision)
	})
	add(server, "rules_delete", "Delete a saved rule, leaving plugin source intact. Requires restart.", false, true, func(_ context.Context, in DeleteInput) (rewrite.Status, error) {
		return rules.DeleteRule(in.ID, in.ExpectedRevision)
	})
	add(server, "rules_test", "Dry-run one rule and one hook against supplied fixture data. No upstream request is sent.", true, false, func(ctx context.Context, in TestInput) (TestOutput, error) {
		input, err := in.Exchange.decode()
		if err != nil {
			return TestOutput{}, err
		}
		out, calls, err := rules.Test(ctx, in.Rule, in.Stage, input)
		return TestOutput{encodeTest(out), calls}, err
	})
	add(server, "plugins_list", "List local plugin manifests, exported hooks, source hashes and compile errors.", true, false, func(ctx context.Context, _ Empty) (PluginList, error) {
		return PluginList{rules.Plugins(ctx), rules.Status().Revision}, nil
	})
	add(server, "plugins_get", "Read an installed plugin's source and metadata before editing.", true, false, func(ctx context.Context, in Identity) (PluginOutput, error) {
		info, source, revision, err := rules.ReadPlugin(ctx, in.ID)
		s := rules.Status()
		return PluginOutput{info, source, revision, revision != s.ActiveRevision}, err
	})
	add(server, "plugins_validate", "Compile-check a single-file JS/TS plugin without saving or running it.", true, false, func(ctx context.Context, in PluginInput) (plugins.Info, error) {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		p, err := plugins.Compile(ctx, in.Manifest, in.Source)
		if err != nil {
			return plugins.Info{}, err
		}
		return p.Info, nil
	})
	add(server, "plugins_put", "Compile-check and save a plugin manifest and source under aicat Home. Requires restart.", false, true, func(ctx context.Context, in PluginInput) (PluginOutput, error) {
		info, revision, err := rules.PutPlugin(ctx, in.Manifest, in.Source, in.ExpectedRevision)
		s := rules.Status()
		return PluginOutput{Plugin: info, Revision: revision, RestartRequired: revision != s.ActiveRevision}, err
	})
	add(server, "plugins_delete", "Move an unreferenced plugin into aicat Home trash. Remove referring rules first; requires restart.", false, true, func(_ context.Context, in DeleteInput) (rewrite.Status, error) {
		err := rules.DeletePlugin(in.ID, in.ExpectedRevision)
		return rules.Status(), err
	})
	add(server, "rewrite_logs", "Read bounded recent execution diagnostics, including when traffic recording is disabled.", true, false, func(context.Context, Empty) (Logs, error) { return Logs{rules.Executions()}, nil })
	add(server, "traffic_list", "Read recent traffic summaries for checking rule effects; limit 1..200.", true, false, func(ctx context.Context, in ListInput) (traffic.Page, error) {
		if in.Limit == 0 {
			in.Limit = 50
		}
		if in.Limit < 1 || in.Limit > 200 {
			return traffic.Page{}, errors.New("limit must be 1..200")
		}
		return flows.List(ctx, traffic.Query{Limit: in.Limit, Before: traffic.ID(in.Before), Host: in.Host, Method: in.Method, Text: in.Query})
	})
	add(server, "traffic_get", "Read original/sent snapshots, execution errors and fixed rule version for a request.", true, false, func(ctx context.Context, in RecordInput) (traffic.Record, error) {
		return flows.Get(ctx, traffic.ID(in.ID))
	})
	add(server, "traffic_body", "Read a retained body prefix as base64 with completeness metadata; at most 1 MiB returned.", true, false, func(ctx context.Context, in BodyInput) (BodyOutput, error) {
		body, err := flows.OpenBody(ctx, traffic.BodyID(in.ID))
		if err != nil {
			return BodyOutput{}, err
		}
		defer body.Reader.Close()
		b, err := io.ReadAll(io.LimitReader(body.Reader, (1<<20)+1))
		truncated := len(b) > 1<<20
		if truncated {
			b = b[:1<<20]
		}
		return BodyOutput{Info: body.Info, Data: base64.StdEncoding.EncodeToString(b), ReturnedTruncated: truncated}, err
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != "" {
			http.Error(w, "browser origins are not allowed", http.StatusForbidden)
			return
		}
		expected := "Bearer " + token
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="aicat"`)
			http.Error(w, "MCP bearer token required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		handler.ServeHTTP(w, r)
	}), nil
}
func loadToken(home string) (string, error) {
	path := filepath.Join(home, "mcp-token")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		bytes := make([]byte, 32)
		if _, err = rand.Read(bytes); err != nil {
			return "", err
		}
		value := hex.EncodeToString(bytes) + "\n"
		f, err := os.CreateTemp(home, ".mcp-token-*")
		if err != nil {
			return "", err
		}
		defer os.Remove(f.Name())
		_, err = f.WriteString(value)
		if err == nil {
			err = f.Sync()
		}
		err = errors.Join(err, f.Close())
		if err != nil {
			return "", err
		}
		if err = os.Link(f.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("MCP credential must be a regular file")
	}
	if err = os.Chmod(path, 0600); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != 32 || !slices.Equal([]byte(strings.ToLower(token)), []byte(token)) {
		return "", errors.New("invalid MCP credential file")
	}
	return token, nil
}
