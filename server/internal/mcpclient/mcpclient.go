// Package mcpclient provides a lightweight MCP (Model Context Protocol) client
// that connects to stdio and HTTP-based MCP servers, discovers tools, and
// executes them. Used by the agentic loop to integrate MCP tools at runtime.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Tool represents a discovered MCP tool.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// Client is a connection to one MCP server.
type Client struct {
	name      string
	transport string // "stdio" | "sse" | "streamable-http"
	logger    *slog.Logger

	// stdio
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex

	// HTTP
	serverURL  string
	httpClient *http.Client

	// state
	tools     []Tool
	nextID    atomic.Int64
	connected bool
}

// Config is the connection configuration for an MCP server.
type Config struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // "stdio" | "sse" | "streamable-http"
	Command   string            `json:"command"`   // for stdio
	Args      []string          `json:"args"`
	URL       string            `json:"url"`       // for HTTP transports
	Env       map[string]string `json:"env"`
}

// New creates and connects to an MCP server.
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}

	c := &Client{
		name:      cfg.Name,
		transport: cfg.Transport,
		logger:    logger,
		serverURL: cfg.URL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	switch cfg.Transport {
	case "stdio":
		if err := c.connectStdio(ctx, cfg); err != nil {
			return nil, fmt.Errorf("mcp stdio connect: %w", err)
		}
	case "sse", "streamable-http":
		c.connected = true // HTTP is stateless
	default:
		return nil, fmt.Errorf("unsupported MCP transport: %s", cfg.Transport)
	}

	// Discover tools
	if err := c.discoverTools(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp discover tools: %w", err)
	}

	return c, nil
}

// Tools returns the discovered tools.
func (c *Client) Tools() []Tool { return c.tools }

// Name returns the server name.
func (c *Client) Name() string { return c.name }

// CallTool invokes a tool on the MCP server and returns the result.
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]any) (string, error) {
	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}

	resp, err := c.send(ctx, req)
	if err != nil {
		return "", err
	}

	if resp.Error != nil {
		return "", fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	// Extract text content from result
	if resp.Result != nil {
		var result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		}
		if json.Unmarshal(resp.Result, &result) == nil {
			var sb strings.Builder
			for _, c := range result.Content {
				if c.Type == "text" {
					sb.WriteString(c.Text)
				}
			}
			return sb.String(), nil
		}
		return string(resp.Result), nil
	}

	return "", nil
}

// Close disconnects from the MCP server.
func (c *Client) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		if c.stdin != nil {
			c.stdin.Close()
		}
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
	c.connected = false
	return nil
}

// --- internal ---

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) connectStdio(ctx context.Context, cfg Config) error {
	args := cfg.Args
	cmd := exec.CommandContext(ctx, cfg.Command, args...)
	cmd.Stderr = os.Stderr

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start MCP server %s: %w", cfg.Name, err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewReader(stdout)
	c.connected = true

	// Send initialize
	initReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID.Add(1),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":   map[string]any{},
			"clientInfo": map[string]any{
				"name":    "aurion-agent",
				"version": "1.0.0",
			},
		},
	}

	resp, err := c.sendStdio(initReq)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	// Send initialized notification
	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	data, _ := json.Marshal(notif)
	c.mu.Lock()
	c.stdin.Write(data)
	c.stdin.Write([]byte("\n"))
	c.mu.Unlock()

	c.logger.Info("MCP stdio connected", "server", cfg.Name)
	return nil
}

func (c *Client) discoverTools(ctx context.Context) error {
	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/list",
	}

	resp, err := c.send(ctx, req)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("tools/list error: %s", resp.Error.Message)
	}

	if resp.Result != nil {
		var result struct {
			Tools []Tool `json:"tools"`
		}
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return fmt.Errorf("parse tools: %w", err)
		}
		c.tools = result.Tools
	}

	c.logger.Info("MCP tools discovered", "server", c.name, "count", len(c.tools))
	return nil
}

func (c *Client) send(ctx context.Context, req jsonRPCRequest) (*jsonRPCResponse, error) {
	switch c.transport {
	case "stdio":
		return c.sendStdio(req)
	case "sse", "streamable-http":
		return c.sendHTTP(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported transport: %s", c.transport)
	}
}

func (c *Client) sendStdio(req jsonRPCRequest) (*jsonRPCResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write to MCP stdin: %w", err)
	}

	// Read response line with a hard timeout so a hung MCP server
	// (e.g. first-time npx download, crashed child, misbehaving
	// server that never answers) cannot block the session loop
	// indefinitely. 45s is generous for initialize on a cold start
	// and plenty for tools/call on a warm connection.
	type readResult struct {
		line []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := c.stdout.ReadBytes('\n')
		ch <- readResult{line: line, err: err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("read from MCP stdout: %w", r.err)
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal(r.line, &resp); err != nil {
			return nil, fmt.Errorf("parse MCP response: %w", err)
		}
		return &resp, nil
	case <-time.After(45 * time.Second):
		return nil, fmt.Errorf("MCP stdio read timeout after 45s (server=%s method=%s)", c.name, req.Method)
	}
}

func (c *Client) sendHTTP(ctx context.Context, req jsonRPCRequest) (*jsonRPCResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serverURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("MCP HTTP request: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read MCP response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP HTTP error %d: %s", httpResp.StatusCode, string(body))
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse MCP response: %w", err)
	}

	return &resp, nil
}
