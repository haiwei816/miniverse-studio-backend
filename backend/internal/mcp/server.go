// Package mcp implements a lightweight Model Context Protocol (JSON-RPC 2.0) server.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	ProtocolVersion = "2025-03-26"
	maxToolTextLen  = 16384
)

// ToolSpec describes a single MCP tool and its JSON Schema parameters.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	ReadOnly    bool           `json:"-"`
}

// ResourceSpec describes a read-only MCP resource.
type ResourceSpec struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

// ToolHandler executes a tool call with JSON arguments.
type ToolHandler func(ctx context.Context, args json.RawMessage) (any, error)

// ResourceHandler reads a resource by URI.
type ResourceHandler func(ctx context.Context) (string, error)

// RPCError represents a JSON-RPC 2.0 protocol error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("rpc(%d): %s", e.Code, e.Message) }

type registeredTool struct {
	spec    ToolSpec
	handler ToolHandler
}

type registeredResource struct {
	spec    ResourceSpec
	handler ResourceHandler
}

// Server dispatches MCP JSON-RPC requests and in-process tool calls.
type Server struct {
	name          string
	version       string
	toolFilter    func(ctx context.Context, tool ToolSpec) bool
	tools         map[string]registeredTool
	toolOrder     []string
	resources     map[string]registeredResource
	resourceOrder []string
}

// NewServer constructs an MCP server with an optional per-request visibility/authorization filter.
func NewServer(name, version string, filter func(ctx context.Context, tool ToolSpec) bool) *Server {
	return &Server{
		name:       name,
		version:    version,
		toolFilter: filter,
		tools:      make(map[string]registeredTool),
		resources:  make(map[string]registeredResource),
	}
}

func (s *Server) RegisterTool(spec ToolSpec, handler ToolHandler) {
	if _, exists := s.tools[spec.Name]; !exists {
		s.toolOrder = append(s.toolOrder, spec.Name)
	}
	s.tools[spec.Name] = registeredTool{spec: spec, handler: handler}
}

func (s *Server) RegisterResource(spec ResourceSpec, handler ResourceHandler) {
	if _, exists := s.resources[spec.URI]; !exists {
		s.resourceOrder = append(s.resourceOrder, spec.URI)
	}
	s.resources[spec.URI] = registeredResource{spec: spec, handler: handler}
}

func (s *Server) allowed(ctx context.Context, spec ToolSpec) bool {
	if s.toolFilter == nil {
		return true
	}
	return s.toolFilter(ctx, spec)
}

// ListTools returns all tools authorized for ctx.
func (s *Server) ListTools(ctx context.Context) []ToolSpec {
	out := make([]ToolSpec, 0, len(s.toolOrder))
	for _, name := range s.toolOrder {
		rt := s.tools[name]
		if s.allowed(ctx, rt.spec) {
			out = append(out, rt.spec)
		}
	}
	return out
}

// CallTool invokes an authorized tool and returns serialized text + structured result.
func (s *Server) CallTool(ctx context.Context, name string, args json.RawMessage) (string, any, bool, *RPCError) {
	rt, exists := s.tools[name]
	if !exists || !s.allowed(ctx, rt.spec) {
		return "", nil, false, &RPCError{Code: -32602, Message: "unknown or unauthorized tool: " + name}
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	res, err := rt.handler(ctx, args)
	if err != nil {
		payload := map[string]any{"error": err.Error()}
		raw, _ := json.Marshal(payload)
		return string(raw), payload, true, nil
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return "", nil, false, &RPCError{Code: -32603, Message: "failed to encode tool response"}
	}
	text := string(raw)
	if len(text) > maxToolTextLen {
		preview := truncateUTF8(text, maxToolTextLen-256)
		truncated, _ := json.Marshal(map[string]any{
			"truncated": true,
			"total_len": len(text),
			"preview":   preview,
		})
		text = string(truncated)
	}
	return text, res, false, nil
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.ValidString(s[:maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

func hasID(id json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(id))
	return trimmed != "" && trimmed != "null"
}

// HandleRequest processes one JSON-RPC 2.0 message. For notifications it returns hasResponse=false.
func (s *Server) HandleRequest(ctx context.Context, raw []byte) ([]byte, bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		resp, _ := json.Marshal(rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &RPCError{Code: -32700, Message: "parse error"},
		})
		return resp, true
	}
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		resp, _ := json.Marshal(rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32600, Message: "invalid jsonrpc version"},
		})
		return resp, true
	}
	if strings.HasPrefix(req.Method, "notifications/") || !hasID(req.ID) {
		return nil, false
	}

	reply := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		reply.Result = map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    s.name,
				"version": s.version,
			},
		}
	case "ping":
		reply.Result = map[string]any{}
	case "tools/list":
		specs := s.ListTools(ctx)
		tools := make([]map[string]any, 0, len(specs))
		for _, t := range specs {
			tools = append(tools, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
				"annotations": map[string]any{
					"readOnlyHint": t.ReadOnly,
				},
			})
		}
		reply.Result = map[string]any{"tools": tools}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if len(req.Params) == 0 || json.Unmarshal(req.Params, &params) != nil || strings.TrimSpace(params.Name) == "" {
			reply.Error = &RPCError{Code: -32602, Message: "invalid tools/call params"}
			break
		}
		text, structured, isErr, rpcErr := s.CallTool(ctx, params.Name, params.Arguments)
		if rpcErr != nil {
			reply.Error = rpcErr
			break
		}
		reply.Result = map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
			"structuredContent": structured,
			"isError":           isErr,
		}
	case "resources/list":
		resources := make([]ResourceSpec, 0, len(s.resourceOrder))
		for _, uri := range s.resourceOrder {
			resources = append(resources, s.resources[uri].spec)
		}
		reply.Result = map[string]any{"resources": resources}
	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if len(req.Params) == 0 || json.Unmarshal(req.Params, &params) != nil || strings.TrimSpace(params.URI) == "" {
			reply.Error = &RPCError{Code: -32602, Message: "invalid resources/read params"}
			break
		}
		rr, exists := s.resources[params.URI]
		if !exists {
			reply.Error = &RPCError{Code: -32002, Message: "resource not found: " + params.URI}
			break
		}
		text, err := rr.handler(ctx)
		if err != nil {
			reply.Error = &RPCError{Code: -32603, Message: err.Error()}
			break
		}
		reply.Result = map[string]any{
			"contents": []map[string]any{
				{
					"uri":      rr.spec.URI,
					"mimeType": rr.spec.MimeType,
					"text":     text,
				},
			},
		}
	default:
		reply.Error = &RPCError{Code: -32601, Message: "method not found: " + req.Method}
	}

	out, _ := json.Marshal(reply)
	return out, true
}

// ServeStdio reads newline-delimited JSON-RPC messages from r and writes responses to w.
func (s *Server) ServeStdio(ctx context.Context, r io.Reader, w io.Writer) error {
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
		resp, hasResp := s.HandleRequest(ctx, []byte(line))
		if !hasResp {
			continue
		}
		if _, err := fmt.Fprintf(w, "%s\n", resp); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
