package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type roleKey struct{}

func newTestServer() *Server {
	s := NewServer("test-mcp", "1.0.0", func(ctx context.Context, tool ToolSpec) bool {
		role, _ := ctx.Value(roleKey{}).(string)
		return tool.ReadOnly || role == "operator"
	})
	s.RegisterTool(ToolSpec{
		Name:        "read_status",
		Description: "Read health",
		ReadOnly:    true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"fail": map[string]any{"type": "boolean"},
			},
		},
	}, func(_ context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	s.RegisterTool(ToolSpec{
		Name:        "write_proposal",
		Description: "Write proposal",
		ReadOnly:    false,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"fail": map[string]any{"type": "boolean"},
			},
		},
	}, func(_ context.Context, args json.RawMessage) (any, error) {
		var b struct {
			Fail bool `json:"fail"`
		}
		_ = json.Unmarshal(args, &b)
		if b.Fail {
			return nil, errors.New("bad proposal")
		}
		return map[string]any{"state": "pending"}, nil
	})
	s.RegisterResource(ResourceSpec{
		URI:      "miniverse://test/runbook",
		Name:     "runbook",
		MimeType: "text/plain",
	}, func(_ context.Context) (string, error) {
		return "SOP content", nil
	})
	return s
}

func TestHandshakeRoleFilteringToolsAndResources(t *testing.T) {
	s := newTestServer()

	respBytes, has := s.HandleRequest(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if !has || !strings.Contains(string(respBytes), `"protocolVersion":"`+ProtocolVersion+`"`) {
		t.Fatalf("unexpected initialize response: has=%v body=%s", has, respBytes)
	}

	if _, has := s.HandleRequest(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); has {
		t.Fatal("notification returned a response")
	}

	readCtx := context.WithValue(context.Background(), roleKey{}, "support")
	listBytes, _ := s.HandleRequest(readCtx, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	var listResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(listBytes, &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Result.Tools) != 1 || listResp.Result.Tools[0].Name != "read_status" {
		t.Fatalf("support role exposed write tool: %+v", listResp.Result.Tools)
	}

	denyBytes, _ := s.HandleRequest(readCtx, []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"write_proposal","arguments":{}}}`))
	if !strings.Contains(string(denyBytes), `"code":-32602`) {
		t.Fatalf("unauthorized write call not rejected: %s", denyBytes)
	}

	opCtx := context.WithValue(context.Background(), roleKey{}, "operator")
	okBytes, _ := s.HandleRequest(opCtx, []byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"write_proposal","arguments":{}}}`))
	if !strings.Contains(string(okBytes), `"isError":false`) || !strings.Contains(string(okBytes), `"pending"`) {
		t.Fatalf("unexpected write tool result: %s", okBytes)
	}
	errBytes, _ := s.HandleRequest(opCtx, []byte(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"write_proposal","arguments":{"fail":true}}}`))
	if !strings.Contains(string(errBytes), `"isError":true`) || !strings.Contains(string(errBytes), "bad proposal") {
		t.Fatalf("expected tool isError=true, got: %s", errBytes)
	}

	resList, _ := s.HandleRequest(readCtx, []byte(`{"jsonrpc":"2.0","id":6,"method":"resources/list"}`))
	if !strings.Contains(string(resList), "miniverse://test/runbook") {
		t.Fatalf("missing resource in list: %s", resList)
	}
	resRead, _ := s.HandleRequest(readCtx, []byte(`{"jsonrpc":"2.0","id":7,"method":"resources/read","params":{"uri":"miniverse://test/runbook"}}`))
	if !strings.Contains(string(resRead), "SOP content") {
		t.Fatalf("unexpected resource content: %s", resRead)
	}
}

func TestServeStdioStreamsMessages(t *testing.T) {
	s := newTestServer()
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_status","arguments":{}}}` + "\n",
	)
	var out bytes.Buffer
	ctx := context.WithValue(context.Background(), roleKey{}, "support")
	if err := s.ServeStdio(ctx, in, &out); err != nil {
		t.Fatalf("ServeStdio failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %q", len(lines), out.String())
	}
}

func TestCallToolValidatesInputSchema(t *testing.T) {
	s := newTestServer()
	opCtx := context.WithValue(context.Background(), roleKey{}, "operator")
	text, _, isErr, rpcErr := s.CallTool(opCtx, "write_proposal", json.RawMessage(`{"fail":"not-a-bool"}`))
	if rpcErr != nil || !isErr || !strings.Contains(text, "invalid_arguments") {
		t.Fatalf("expected schema rejection invalid_arguments, got text=%q isErr=%v rpcErr=%v", text, isErr, rpcErr)
	}
}
