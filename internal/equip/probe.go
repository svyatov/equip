package equip

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// probeTimeout is how long a probe gives an MCP server to answer, as
	// Claude Code gives it to connect.
	probeTimeout = 5 * time.Second
	// discoverTimeout is how long a probe waits for server/discover before
	// it takes a silent server for one that speaks only the legacy protocol.
	// ponytail: a modern server slower than this to start gets initialize
	// too; wait on both answers if such servers show up.
	discoverTimeout = 2 * time.Second
	// mcpTextChars is the characters Claude Code keeps of a server's
	// instructions and of a tool's description.
	mcpTextChars = 2048
	// killDelay is how long a probe waits for a killed server's output to
	// close.
	killDelay = time.Second
)

// The MCP protocol versions equip speaks: the modern one first, then the
// legacy one with an initialize handshake.
const (
	modernVersion = "2026-07-28"
	legacyVersion = "2025-11-25"
	// jsonRPC is the JSON-RPC version of every MCP message.
	jsonRPC = "2.0"
)

var (
	// errServerClosed is the error of a request the MCP server stopped
	// before answering.
	errServerClosed = errors.New("the MCP server closed its output")
	// errRefused is the error of a request a remote MCP server answered
	// with an HTTP error.
	errRefused = errors.New("the MCP server refused")
)

// mcpConn is a connection to an MCP server.
type mcpConn interface {
	// call sends the request method with params and decodes its result into
	// result. It skips every other message the server sends.
	call(ctx context.Context, method string, params, result any) error
	// notify sends the notification method.
	notify(ctx context.Context, method string) error
	// close ends the connection, and the server with it when equip started
	// it.
	close()
}

// serverConfig is how to start an MCP server, read from its config in an
// agent.
type serverConfig struct {
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"` // a remote server's, in Claude Code
	Command string            `json:"command,omitempty"`
	Dir     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`  // a remote server's
	Type    string            `json:"type,omitempty"` // Claude Code's transport: stdio, http or sse
	Args    []string          `json:"args,omitempty"`
	// AlwaysLoad exempts the server's tools from Claude Code's tool search.
	AlwaysLoad bool `json:"alwaysLoad,omitempty"`
}

// measurement is what a probe read from an MCP server.
type measurement struct {
	TTLMs        *int64    `json:"ttlMs,omitempty"` // how long the server lets equip cache it; missing is for good
	Instructions string    `json:"instructions"`
	Tools        []mcpTool `json:"tools"`
}

// mcpTool is one tool an MCP server lists.
type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// cost estimates the tokens the MCP server named name, measured as m, puts
// into agent's sessions. With a tool search, Codex lists the server's name
// and instructions, and Claude Code its instructions and the names of its
// tools. Claude Code with full, no tool search, lists every tool's
// definition. ponytail: Codex always counts as searching, as every model it
// ships with does.
func (m measurement) cost(agent Agent, name string, full bool) int {
	if agent == Codex {
		return tokens(agent, len(name)+len(m.Instructions))
	}

	size := len(truncate(m.Instructions, mcpTextChars))
	for _, tool := range m.Tools {
		size += len("mcp__" + name + "__" + tool.Name)
		if full {
			size += len(truncate(tool.Description, mcpTextChars)) + len(tool.InputSchema)
		}
	}

	return tokens(agent, size)
}

// claudeToolSearch reports whether Claude Code defers MCP tools behind its
// tool search, as it does unless ENABLE_TOOL_SEARCH is false. ponytail: reads
// the environment only, not the env of Claude Code's settings or the auto
// modes; read those if users set it there.
func claudeToolSearch(machine Machine) bool {
	return !slices.Contains(machine.Env, "ENABLE_TOOL_SEARCH=false")
}

// truncate is s cut to its first n characters.
func truncate(s string, n int) string {
	runes := []rune(s)

	return string(runes[:min(len(runes), n)])
}

// probe starts the MCP server cfg with env and reads its instructions and
// tools. It tries the modern server/discover first and falls back to the
// legacy initialize handshake.
func probe(ctx context.Context, env []string, cfg serverConfig) (measurement, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var none measurement

	var conn mcpConn = &httpConn{cfg: cfg, session: "", id: 0}
	if cfg.URL == "" {
		stdio, err := dialStdio(ctx, env, cfg)
		if err != nil {
			return none, err
		}

		conn = stdio
	}
	defer conn.close()

	measured, meta, err := handshake(ctx, conn)
	if err != nil {
		return none, err
	}

	return listTools(ctx, conn, measured, meta)
}

// listTools adds the tools the server on conn lists, on every page, to
// measured. meta is the _meta of a modern server's requests, or nil.
func listTools(ctx context.Context, conn mcpConn, measured measurement, meta map[string]any) (measurement, error) {
	var none measurement

	params := map[string]any{}
	if meta != nil {
		params["_meta"] = meta
	}

	for {
		var page struct {
			TTLMs      *int64    `json:"ttlMs"`
			NextCursor string    `json:"nextCursor"`
			Tools      []mcpTool `json:"tools"`
		}

		err := conn.call(ctx, "tools/list", params, &page)
		if err != nil {
			return none, err
		}

		measured.Tools = append(measured.Tools, page.Tools...)
		if measured.TTLMs == nil || page.TTLMs != nil && *page.TTLMs < *measured.TTLMs {
			measured.TTLMs = page.TTLMs
		}

		if page.NextCursor == "" {
			return measured, nil
		}

		params["cursor"] = page.NextCursor
	}
}

// handshake opens the session with the server on conn and reads its
// instructions. It returns the _meta of a modern server's requests, or none
// for a legacy server.
func handshake(ctx context.Context, conn mcpConn) (measurement, map[string]any, error) {
	var info struct {
		TTLMs        *int64 `json:"ttlMs"`
		Instructions string `json:"instructions"`
	}

	meta := map[string]any{
		"io.modelcontextprotocol/protocolVersion":    modernVersion,
		"io.modelcontextprotocol/clientInfo":         clientInfo(),
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}

	discoverCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	err := conn.call(discoverCtx, "server/discover", map[string]any{"_meta": meta}, &info)
	if err != nil {
		meta = nil

		err = conn.call(ctx, "initialize", map[string]any{
			"protocolVersion": legacyVersion, "capabilities": map[string]any{}, "clientInfo": clientInfo(),
		}, &info)
		if err == nil {
			err = conn.notify(ctx, "notifications/initialized")
		}
	}

	return measurement{TTLMs: info.TTLMs, Instructions: info.Instructions, Tools: nil}, meta, err
}

// cacheEntry is a measurement as equip caches it.
type cacheEntry struct {
	At       time.Time   `json:"at"`
	Measured measurement `json:"measurement"`
}

// cachePath is the file that caches the measurement of the MCP server cfg,
// named by a hash of cfg, so a server whose config changes is measured again.
func cachePath(machine Machine, cfg serverConfig) string {
	data, _ := json.Marshal(cfg) //nolint:errchkjson // strings always encode
	sum := sha256.Sum256(data)

	return filepath.Join(machine.CacheHome, "equip", "mcp-"+hex.EncodeToString(sum[:])+".json")
}

// writeCache caches m, the measurement of the MCP server cfg.
func writeCache(machine Machine, cfg serverConfig, m measurement) error {
	data, err := json.Marshal(cacheEntry{At: time.Now(), Measured: m})
	if err != nil {
		return fmt.Errorf("encode the measurement: %w", err)
	}

	return writeFile(cachePath(machine, cfg), data)
}

// readCache reads the cached measurement of the MCP server cfg, reporting
// whether there is one. A cache equip cannot read has none.
func readCache(machine Machine, cfg serverConfig) (measurement, bool) {
	var entry cacheEntry

	data, err := os.ReadFile(cachePath(machine, cfg))
	if err != nil || json.Unmarshal(data, &entry) != nil {
		return entry.Measured, false
	}

	ttl := entry.Measured.TTLMs
	fresh := ttl == nil || time.Now().Before(entry.At.Add(time.Duration(*ttl)*time.Millisecond))

	return entry.Measured, fresh
}

// message is the notification method.
func message(method string) map[string]any {
	return map[string]any{"jsonrpc": jsonRPC, "method": method}
}

// request is the request method with params, numbered id.
func request(id int, method string, params any) map[string]any {
	msg := message(method)
	msg["id"], msg["params"] = id, params

	return msg
}

// clientInfo is how equip names itself to an MCP server.
func clientInfo() map[string]string { return map[string]string{"name": "equip", "version": "0"} }

// stdioConn is a connection to an MCP server over its standard streams.
type stdioConn struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	lines <-chan []byte // the server's output, one message per line
	id    int           // of the last request
}

// dialStdio starts the MCP server cfg with env. The server stops when ctx
// ends.
func dialStdio(ctx context.Context, env []string, cfg serverConfig) (*stdioConn, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...) //nolint:gosec // the agent already runs this server
	cmd.Dir = cfg.Dir
	cmd.WaitDelay = killDelay
	// A copy, so the server's variables do not land in env's spare room.
	cmd.Env = slices.Clone(env)

	for key, value := range cfg.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", cfg.Command, err)
	}

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", cfg.Command, err)
	}

	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", cfg.Command, err)
	}

	lines := make(chan []byte)

	go func() {
		defer close(lines)

		reader := bufio.NewReader(out)

		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				select {
				case lines <- line:
				case <-ctx.Done():
					return
				}
			}

			if err != nil {
				return
			}
		}
	}()

	return &stdioConn{cmd: cmd, in: stdin, lines: lines, id: 0}, nil
}

// send writes msg to the server.
func (c *stdioConn) send(msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err == nil {
		_, err = c.in.Write(append(data, '\n'))
	}

	if err != nil {
		return fmt.Errorf("send %s: %w", msg["method"], err)
	}

	return nil
}

func (c *stdioConn) notify(_ context.Context, method string) error { return c.send(message(method)) }

func (c *stdioConn) call(ctx context.Context, method string, params, result any) error {
	c.id++

	err := c.send(request(c.id, method, params))
	if err != nil {
		return err
	}

	for {
		var line []byte

		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", method, ctx.Err())
		case line = <-c.lines:
		}

		if line == nil {
			return fmt.Errorf("%s: %w", method, errServerClosed)
		}

		if answered, err := decodeResponse(line, c.id, method, result); answered {
			return err
		}
	}
}

// close ends the server: it closes its input, as MCP asks, then kills it.
func (c *stdioConn) close() {
	_ = c.in.Close()
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
}

// httpConn is a connection to a remote MCP server over streamable HTTP.
type httpConn struct {
	session string // the Mcp-Session-Id the server gave, if any
	cfg     serverConfig
	id      int // of the last request
}

// close does nothing: each request's answer is closed as it is read.
func (*httpConn) close() {}

func (c *httpConn) notify(ctx context.Context, method string) error {
	resp, err := c.post(ctx, method, message(method), legacyVersion)
	if err != nil {
		return err
	}

	_ = resp.Body.Close()

	return nil
}

func (c *httpConn) call(ctx context.Context, method string, params, result any) error {
	c.id++
	// A modern request names its version in its _meta, and in a header.
	version := legacyVersion
	if p, ok := params.(map[string]any); ok && p["_meta"] != nil {
		version = modernVersion
	}

	resp, err := c.post(ctx, method, request(c.id, method, params), version)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// The answer is one JSON message, or an event stream whose data lines
	// hold messages. ponytail: takes an event's data from one line; join
	// multi-line data if a server splits it.
	reader := bufio.NewReader(resp.Body)
	stream := strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream")

	for {
		line, readErr := reader.ReadBytes('\n')

		data, isData := bytes.CutPrefix(line, []byte("data:"))
		if !stream {
			data, isData = line, true
		}

		if answered, err := decodeResponse(data, c.id, method, result); isData && answered {
			return err
		}

		if readErr != nil {
			return fmt.Errorf("%s: %w", method, errServerClosed)
		}
	}
}

// post sends msg, the request method, to the server with the protocol
// version, and returns its answer.
func (c *httpConn) post(
	ctx context.Context, method string, msg map[string]any, version string,
) (*http.Response, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", version)

	for key, value := range c.cfg.Headers {
		req.Header.Set(key, value)
	}

	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()

		return nil, fmt.Errorf("%s: %w: %s", method, errRefused, resp.Status)
	}

	c.session = cmp.Or(resp.Header.Get("Mcp-Session-Id"), c.session)

	return resp, nil
}

// decodeResponse decodes data, a message from an MCP server, into result
// when it answers the request method, numbered request. It reports whether it
// does, with the request's error.
func decodeResponse(data []byte, request int, method string, result any) (bool, error) {
	var resp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
	}

	answers := json.Unmarshal(data, &resp) == nil && string(resp.ID) == strconv.Itoa(request)
	if !answers {
		return false, nil
	}

	if resp.Error != nil {
		return true, fmt.Errorf("%s: %s", method, resp.Error.Message) //nolint:err113 // the server's own message
	}

	err := json.Unmarshal(resp.Result, result)
	if err != nil {
		return true, fmt.Errorf("%s: %w", method, err)
	}

	return true, nil
}
