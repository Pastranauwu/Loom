// Package mcp speaks the Model Context Protocol over stdio: JSON-RPC 2.0,
// one message per line. It only moves messages; what a tool does is the
// caller's business.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const protocolVersion = "2024-11-05"

// Tool is one callable function. Call returns the text the client sees;
// an error becomes an isError result, not a protocol error.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Call        func(args map[string]any) (string, error)
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads requests from in until EOF and writes replies to out. Each
// request runs in its own goroutine so a slow tool does not block ping.
func Serve(in io.Reader, out io.Writer, name, version string, tools []Tool) error {
	byName := make(map[string]Tool, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	enc := json.NewEncoder(out)
	send := func(r response) {
		r.JSONRPC = "2.0"
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(r)
	}

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			send(response{ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if len(req.ID) == 0 { // notification: no reply
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			send(handle(req, byName, tools, name, version))
		}()
	}
	wg.Wait()
	return sc.Err()
}

func handle(req request, byName map[string]Tool, tools []Tool, name, version string) response {
	res := response{ID: req.ID}
	switch req.Method {
	case "initialize":
		res.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": name, "version": version},
		}
	case "ping":
		res.Result = map[string]any{}
	case "tools/list":
		list := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.Schema})
		}
		res.Result = map[string]any{"tools": list}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			res.Error = &rpcError{-32602, "invalid params"}
			return res
		}
		t, ok := byName[p.Name]
		if !ok {
			res.Error = &rpcError{-32602, fmt.Sprintf("unknown tool %q", p.Name)}
			return res
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		text, err := t.Call(p.Arguments)
		if err != nil {
			text = err.Error()
		}
		res.Result = map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": err != nil,
		}
	default:
		res.Error = &rpcError{-32601, "method not found"}
	}
	return res
}
