package equip_test

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

// fakeMCPEnv names the variable that makes the test binary a fake stdio MCP
// server, with its value as the mode.
const fakeMCPEnv = "EQUIP_FAKE_MCP"

// A TestMain that returns exits with m.Run's code, or 0 when it never ran.
func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeMCPEnv); mode != "" {
		serveFakeMCP(mode)

		return
	}

	m.Run()
}

// fakeRequest is a request to the fake MCP server.
type fakeRequest struct {
	Method string          `json:"method"`
	ID     json.RawMessage `json:"id"`
	Params struct {
		Cursor string          `json:"cursor"`
		Meta   json.RawMessage `json:"_meta"` //nolint:tagliatelle // MCP's own name
	} `json:"params"`
}

// fakeAnswer is the result or error the fake server answers req with, and a
// ttlMs of ttl where the answer has one.
type fakeAnswer func(req fakeRequest, ttl *json.Number) map[string]any

// serveFakeMCP answers MCP requests on stdin as mode says, each after a log
// notification:
//   - legacy: rejects server/discover, answers initialize, and lists the
//     tools a and b on two pages, the last with a ttlMs of $EQUIP_FAKE_TTL
//     when set;
//   - quiet: legacy, but never answers server/discover;
//   - modern: answers server/discover with the instructions
//     $EQUIP_FAKE_INSTRUCTIONS or "Use fake." and a ttlMs of $EQUIP_FAKE_TTL
//     when set, lists the tool a with a ttlMs of an hour, and rejects
//     initialize and every request without the modern _meta;
//   - broken: answers every request with the error boom;
//   - slow: never answers.
//
// With $EQUIP_FAKE_CWD set, it first writes its working dir to that file.
func serveFakeMCP(mode string) {
	answer := map[string]fakeAnswer{
		"legacy": legacyAnswer, "quiet": legacyAnswer, "modern": modernAnswer,
		"broken": func(fakeRequest, *json.Number) map[string]any {
			return map[string]any{"error": map[string]any{"code": -32603, "message": "boom"}}
		},
	}[mode]
	stdin := bufio.NewScanner(os.Stdin)
	stdout := json.NewEncoder(os.Stdout)
	// No ttlMs encodes as null, which a client reads as none.
	var ttl *json.Number

	if value := os.Getenv("EQUIP_FAKE_TTL"); value != "" {
		ttl = new(json.Number(value))
	}

	if path := os.Getenv("EQUIP_FAKE_CWD"); path != "" {
		dir, _ := os.Getwd()
		_ = os.WriteFile(path, []byte(dir), 0o600) //nolint:gosec // the test that starts the fake names the file
	}

	for stdin.Scan() {
		var req fakeRequest

		_ = json.Unmarshal(stdin.Bytes(), &req)
		if req.ID == nil || answer == nil || mode == "quiet" && req.Method == "server/discover" {
			continue
		}

		resp := answer(req, ttl)
		resp["jsonrpc"], resp["id"] = "2.0", req.ID
		log := map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]any{"data": "hi"}}

		// An encode error means stdout closed, so the server stops.
		err := errors.Join(stdout.Encode(log), stdout.Encode(resp))
		if err != nil {
			return
		}
	}
}

func legacyAnswer(req fakeRequest, ttl *json.Number) map[string]any {
	switch {
	case req.Method == "server/discover":
		return map[string]any{"error": map[string]any{"code": -32601, "message": "method not found"}}
	case req.Method == "initialize":
		return map[string]any{"result": map[string]any{"protocolVersion": "2025-11-25", "instructions": "Use fake."}}
	case req.Params.Cursor == "":
		return map[string]any{"result": map[string]any{"tools": []any{fakeTool("a")}, "nextCursor": "2"}}
	}

	return map[string]any{"result": map[string]any{"tools": []any{fakeTool("b")}, "ttlMs": ttl}}
}

func modernAnswer(req fakeRequest, ttl *json.Number) map[string]any {
	switch {
	case req.Method == "initialize" || req.Params.Meta == nil:
		return map[string]any{"error": map[string]any{"code": -32022, "message": "Unsupported protocol version"}}
	case req.Method == "server/discover":
		instructions := cmp.Or(os.Getenv("EQUIP_FAKE_INSTRUCTIONS"), "Use fake.")

		return map[string]any{"result": map[string]any{"instructions": instructions, "ttlMs": ttl}}
	}

	return map[string]any{"result": map[string]any{"tools": []any{fakeTool("a")}, "ttlMs": time.Hour.Milliseconds()}}
}

// fakeTool is the tool name as the fake server lists it: a 6 byte
// description and a 17 byte input schema.
func fakeTool(name string) map[string]any {
	return map[string]any{"name": name, "description": "Does a", "inputSchema": json.RawMessage(`{"type":"object"}`)}
}

// fakeConfig is the config of an MCP server run as the fake server in mode,
// with a ttlMs of ttl when it is not empty.
func fakeConfig(t *testing.T, mode, ttl string) map[string]any {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	return map[string]any{"command": exe, "env": map[string]string{fakeMCPEnv: mode, "EQUIP_FAKE_TTL": ttl}}
}

// writeJSON writes value as JSON to path.
func writeJSON(t *testing.T, path string, value any) {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, path, string(data))
}

// fakeServer adds the user MCP server fake to Claude Code, run as the fake
// server in mode, with a ttlMs of ttl when it is not empty.
func fakeServer(t *testing.T, machine *equiptest.Machine, mode, ttl string) {
	t.Helper()
	writeJSON(t, claudeJSON(machine), map[string]any{"mcpServers": map[string]any{"fake": fakeConfig(t, mode, ttl)}})
}

func TestMCPServerCostIsUnknownBeforeAnyProbe(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "legacy", "")

	if got := row(t, open(t, machine, repo), "fake"); !got.CostUnknown {
		t.Errorf("row = %+v, want its cost unknown", got)
	}
}

func TestProbeMeasuresALegacyServerOnEveryToolsPage(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "legacy", "")

	// "Use fake." and the names mcp__fake__a and mcp__fake__b: 33 bytes.
	if got := probedRow(t, machine, repo); got.CostUnknown || got.Cost != 11 {
		t.Errorf("row = %+v, want a cost of 11", got)
	}
}

func TestProbeMeasuresAModernServerThroughServerDiscover(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "modern", "")

	// "Use fake." and the name mcp__fake__a: 21 bytes.
	if got := probedRow(t, machine, repo); got.CostUnknown || got.Cost != 7 {
		t.Errorf("row = %+v, want a cost of 7", got)
	}
}

func TestProbeOfASilentServerGivesUpAfterFiveSeconds(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "slow", "")
	session := newSession(t, machine, repo)

	start := time.Now()
	err := session.ProbeCost("mcp:fake")()

	if took := time.Since(start); err == nil || took < 5*time.Second || took > 7*time.Second {
		t.Errorf("probe took %v and returned %v, want an error after 5s", took, err)
	}

	if got := row(t, session.View(), "fake"); !got.CostUnknown {
		t.Errorf("row = %+v, want its cost unknown", got)
	}
}

// probedRow probes the MCP server fake in a session of repo, failing the test
// on error, and returns its row.
func probedRow(t *testing.T, machine *equiptest.Machine, repo string) equip.Row {
	t.Helper()

	session := newSession(t, machine, repo)

	err := session.ProbeCost("mcp:fake")()
	if err != nil {
		t.Fatal(err)
	}

	return row(t, session.View(), "fake")
}

func TestProbeTakesAServerSilentOnServerDiscoverForALegacyOne(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "quiet", "")

	if got := probedRow(t, machine, repo); got.CostUnknown || got.Cost != 11 {
		t.Errorf("row = %+v, want a cost of 11", got)
	}
}

func TestProbeSaysWhatTheServerAnsweredWithAnError(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "broken", "")

	err := newSession(t, machine, repo).ProbeCost("mcp:fake")()
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("ProbeCost = %v, want the server's error boom", err)
	}
}

func TestProbeStartsAServerAsCodexDoesWhenOnlyCodexHasItOn(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeCodexServer(t, machine)
	// Claude Code has it off, and could not start it anyway.
	writeJSON(t, claudeJSON(machine), map[string]any{
		"mcpServers": map[string]any{"fake": map[string]any{"command": "no-such-command"}},
		"projects":   map[string]any{repo: map[string]any{"disabledMcpServers": []string{"fake"}}},
	})

	if got := probedRow(t, machine, repo); got.CostUnknown {
		t.Errorf("row = %+v, want its cost measured", got)
	}
}

func TestProbeStartsAServerInTheProject(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	cwd := filepath.Join(machine.Root, "cwd")
	config := fakeConfig(t, "modern", "")
	config["env"] = map[string]string{fakeMCPEnv: "modern", "EQUIP_FAKE_CWD": cwd}
	writeJSON(t, claudeJSON(machine), map[string]any{"mcpServers": map[string]any{"fake": config}})
	probedRow(t, machine, repo)

	if got, _ := os.ReadFile(cwd); string(got) != repo {
		t.Errorf("server ran in %q, want %q", got, repo)
	}
}

func TestProbeStartsAServerInTheDirItsConfigNames(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	cwd, dir := filepath.Join(machine.Root, "cwd"), machine.Mkdir(filepath.Join(machine.Root, "server"))
	config := fakeConfig(t, "modern", "")
	config["env"] = map[string]string{fakeMCPEnv: "modern", "EQUIP_FAKE_CWD": cwd}
	config["cwd"] = dir
	writeJSON(t, claudeJSON(machine), map[string]any{"mcpServers": map[string]any{"fake": config}})
	probedRow(t, machine, repo)

	if got, _ := os.ReadFile(cwd); string(got) != dir {
		t.Errorf("server ran in %q, want %q", got, dir)
	}
}

func TestUserServerMeasuredInOneProjectShowsItsCostInAnother(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	fakeServer(t, machine, "modern", "")
	probedRow(t, machine, machine.Repo("app"))

	if got := row(t, open(t, machine, machine.Repo("other")), "fake"); got.CostUnknown {
		t.Errorf("row = %+v, want the cost measured in app", got)
	}
}

func TestClaudeCodeCostsTheFirst2048CharactersOfInstructions(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	config := fakeConfig(t, "modern", "")
	config["env"] = map[string]string{fakeMCPEnv: "modern", "EQUIP_FAKE_INSTRUCTIONS": strings.Repeat("é", 3000)}
	writeJSON(t, claudeJSON(machine), map[string]any{"mcpServers": map[string]any{"fake": config}})

	// 2048 two-byte characters and mcp__fake__a: 4108 bytes.
	if got := probedRow(t, machine, repo); got.Cost != 1370 {
		t.Errorf("row = %+v, want a cost of 1370", got)
	}
}

func TestProbeRefusesAServerOnTheSSETransport(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeJSON(t, claudeJSON(machine), map[string]any{"mcpServers": map[string]any{
		"fake": map[string]any{"type": "sse", "url": "http://127.0.0.1:1/sse"},
	}})

	err := newSession(t, machine, repo).ProbeCost("mcp:fake")()
	if !errors.Is(err, equip.ErrCannotProbe) {
		t.Errorf("ProbeCost = %v, want ErrCannotProbe", err)
	}
}

func TestServerWhoseConfigChangedIsUnknownAgain(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "modern", "")
	probedRow(t, machine, repo)

	config := fakeConfig(t, "modern", "")
	config["args"] = []string{"--new"}
	writeJSON(t, claudeJSON(machine), map[string]any{"mcpServers": map[string]any{"fake": config}})

	if got := row(t, open(t, machine, repo), "fake"); !got.CostUnknown {
		t.Errorf("row = %+v, want its cost unknown", got)
	}
}

func TestCodexServerStaysMeasuredWhenItsStateChanges(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeCodexServer(t, machine)
	probedRow(t, machine, repo)

	writeFile(t, machine.CodexConfig(), fakeCodexServerConfig(t)+"enabled = true\n")

	if got := row(t, open(t, machine, repo), "fake"); got.CostUnknown {
		t.Errorf("row = %+v, want its cost still measured", got)
	}
}

func TestMeasuredServerCostsNothingInAnAgentThatDoesNotHaveIt(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "modern", "")
	probedRow(t, machine, repo)

	if got := open(t, machine, repo).Totals[equip.Codex]; got != 0 {
		t.Errorf("Codex total = %d, want 0 for a Claude Code server", got)
	}
}

func TestMeasuredCostShowsOnTheNextOpenWithoutAProbe(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "modern", "")
	probedRow(t, machine, repo)

	if got := row(t, open(t, machine, repo), "fake"); got.CostUnknown || got.Cost != 7 {
		t.Errorf("row = %+v, want the cached cost of 7", got)
	}
}

func TestMeasuredCostIsUnknownAgainOnceItsTTLHasPassed(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "modern", "0")
	probedRow(t, machine, repo)

	if got := row(t, open(t, machine, repo), "fake"); !got.CostUnknown {
		t.Errorf("row = %+v, want its cost unknown", got)
	}
}

func TestMeasuredCostHonoursTheTTLOfAToolsPage(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "legacy", "0")
	probedRow(t, machine, repo)

	if got := row(t, open(t, machine, repo), "fake"); !got.CostUnknown {
		t.Errorf("row = %+v, want its cost unknown", got)
	}
}

func TestProbeRefusesAServerTheAgentHasOff(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeJSON(t, claudeJSON(machine), map[string]any{
		"mcpServers": map[string]any{"fake": fakeConfig(t, "modern", "")},
		"projects":   map[string]any{repo: map[string]any{"disabledMcpServers": []string{"fake"}}},
	})
	session := newSession(t, machine, repo)
	// On only in equip, until a save.
	session.SetState("mcp:fake", equip.On)

	err := session.ProbeCost("mcp:fake")()
	if !errors.Is(err, equip.ErrCannotProbe) {
		t.Errorf("ProbeCost = %v, want ErrCannotProbe", err)
	}
}

func TestProbeRefusesAProjectServerTheUserHasNotApproved(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeJSON(t, filepath.Join(repo, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{"fake": fakeConfig(t, "modern", "")},
	})

	err := newSession(t, machine, repo).ProbeCost("mcp:fake")()
	if !errors.Is(err, equip.ErrCannotProbe) {
		t.Errorf("ProbeCost = %v, want ErrCannotProbe", err)
	}
}

// fakePlugin installs the Claude Code plugin github@official with the MCP
// server fake, run as the fake server in modern mode.
func fakePlugin(t *testing.T, machine *equiptest.Machine) {
	t.Helper()
	writeJSON(t, filepath.Join(machine.Plugin("github@official", "user", ""), ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{"fake": fakeConfig(t, "modern", "")},
	})
}

// content returns the content name among the contents of the plugin key.
func content(t *testing.T, session *equip.Session, key, name string) equip.Content {
	t.Helper()

	contents := session.Detail(key).Contents

	i := slices.IndexFunc(contents, func(c equip.Content) bool { return c.Name == name })
	if i < 0 {
		t.Fatalf("Contents = %+v, want %s among them", contents, name)
	}

	return contents[i]
}

func TestProbeStartsAPluginServerFromThePluginsDir(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	dir := machine.Plugin("github@official", "user", "")
	exe, _ := fakeConfig(t, "modern", "")["command"].(string)

	err := os.Symlink(exe, filepath.Join(dir, "fake"))
	if err != nil {
		t.Fatal(err)
	}

	writeJSON(t, filepath.Join(dir, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"fake": map[string]any{
		"command": "${CLAUDE_PLUGIN_ROOT}/fake", "env": map[string]string{fakeMCPEnv: "${FAKE_MODE:-modern}"},
	}}})
	session := newSession(t, machine, repo)

	err = session.ProbeCost("mcp:github@official:fake")()
	if err != nil {
		t.Fatal(err)
	}

	if got := content(t, session, "github@official", "fake"); got.CostUnknown {
		t.Errorf("content = %+v, want its cost measured", got)
	}
}

func TestProbeExpandsTheVariablesAClaudeCodeConfigNames(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Env = append(machine.Env, "FAKE_MODE=modern")
	repo := machine.Repo("app")
	config := fakeConfig(t, "modern", "")
	config["env"] = map[string]string{fakeMCPEnv: "${FAKE_MODE}"}
	writeJSON(t, claudeJSON(machine), map[string]any{"mcpServers": map[string]any{"fake": config}})

	if got := probedRow(t, machine, repo); got.CostUnknown {
		t.Errorf("row = %+v, want its cost measured", got)
	}
}

func TestServerNotMeasuredYetLeavesItsPluginAndTotalPartial(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakePlugin(t, machine)
	view := open(t, machine, repo)

	if got := row(t, view, "github@official"); !got.CostUnknown || !view.Unknown[equip.ClaudeCode] {
		t.Errorf("plugin row = %+v and Unknown = %v, want both partial in Claude Code", got, view.Unknown)
	}
}

func TestServerThatIsOffLeavesTheTotalComplete(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeServer(t, machine, "modern", "")
	session := newSession(t, machine, repo)
	session.SetState("mcp:fake", equip.Off)

	if got := session.View().Unknown; got[equip.ClaudeCode] {
		t.Errorf("Unknown = %v, want Claude Code's total complete", got)
	}
}

func TestMeasuredPluginServerAddsItsCostToThePlugin(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakePlugin(t, machine)
	session := newSession(t, machine, repo)

	if got := content(t, session, "github@official", "fake"); !got.CostUnknown {
		t.Errorf("content = %+v before the probe, want its cost unknown", got)
	}

	err := session.ProbeCost("mcp:github@official:fake")()
	if err != nil {
		t.Fatal(err)
	}

	if got := content(t, session, "github@official", "fake"); got.CostUnknown || got.Cost != 7 {
		t.Errorf("content = %+v, want a cost of 7", got)
	}

	if got := row(t, session.View(), "github@official"); got.Cost != 7 || got.CostUnknown {
		t.Errorf("plugin row = %+v, want a cost of 7", got)
	}
}

func TestProbeRefusesAServerWhosePluginIsOff(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakePlugin(t, machine)
	writeJSON(t, filepath.Join(machine.Home, ".claude", "settings.json"),
		map[string]any{"enabledPlugins": map[string]bool{"github@official": false}})

	err := newSession(t, machine, repo).ProbeCost("mcp:github@official:fake")()
	if !errors.Is(err, equip.ErrCannotProbe) {
		t.Errorf("ProbeCost = %v, want ErrCannotProbe", err)
	}
}

// fakeCodexServerConfig is the Codex config table of the MCP server fake, run
// as the fake server in modern mode.
func fakeCodexServerConfig(t *testing.T) string {
	t.Helper()

	exe, _ := fakeConfig(t, "modern", "")["command"].(string)

	return "[mcp_servers.fake]\ncommand = " + strconv.Quote(exe) + "\nenv = { " + fakeMCPEnv + " = \"modern\" }\n"
}

// fakeCodexServer adds the MCP server fake to the user's Codex config, run as
// the fake server in modern mode.
func fakeCodexServer(t *testing.T, machine *equiptest.Machine) {
	t.Helper()
	writeFile(t, machine.CodexConfig(), fakeCodexServerConfig(t))
}

func TestCodexCostsAServersNameAndInstructions(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeCodexServer(t, machine)
	probedRow(t, machine, repo)

	// fake and "Use fake.": 13 bytes, with no skills intro as no skill is on.
	if got := open(t, machine, repo).Totals[equip.Codex]; got != 4 {
		t.Errorf("Codex total = %d, want 4", got)
	}
}

func TestProbeStartsACodexServerFromItsMergedLayers(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	exe, _ := fakeConfig(t, "modern", "")["command"].(string)
	// Without the project layer's env, the test binary runs no tests.
	writeFile(t, machine.CodexConfig(),
		"[mcp_servers.fake]\ncommand = "+strconv.Quote(exe)+"\nargs = [\"-test.run=^$\"]\n")
	trust(t, machine, repo)
	writeFile(t, codexProject(repo), "[mcp_servers.fake]\nenv = { "+fakeMCPEnv+" = \"modern\" }\n")

	if got := probedRow(t, machine, repo); got.CostUnknown {
		t.Errorf("row = %+v, want its cost measured", got)
	}
}

func TestServerNotMeasuredYetCostsNothing(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	fakeCodexServer(t, machine)
	view := open(t, machine, repo)

	if got := row(t, view, "fake"); got.Cost != 0 || view.Totals[equip.Codex] != 0 {
		t.Errorf("row = %+v and Codex total %d, want both 0", got, view.Totals[equip.Codex])
	}
}

func TestClaudeCodeCostsFullToolDefinitionsWithToolSearchOff(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Env = append(machine.Env, "ENABLE_TOOL_SEARCH=false")
	repo := machine.Repo("app")
	fakeServer(t, machine, "modern", "")
	probedRow(t, machine, repo)

	// "Use fake.", then mcp__fake__a, "Does a" and {"type":"object"}: 44 bytes.
	if got := row(t, open(t, machine, repo), "fake"); got.Cost != 15 {
		t.Errorf("row = %+v, want a cost of 15", got)
	}
}

func TestClaudeCodeCostsFullToolDefinitionsOfAnAlwaysLoadedServer(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	config := fakeConfig(t, "modern", "")
	config["alwaysLoad"] = true
	writeJSON(t, claudeJSON(machine), map[string]any{"mcpServers": map[string]any{"fake": config}})
	probedRow(t, machine, repo)

	if got := row(t, open(t, machine, repo), "fake"); got.Cost != 15 {
		t.Errorf("row = %+v, want a cost of 15", got)
	}
}

// fakeHTTPServer starts a legacy MCP server on streamable HTTP that wants the
// header X-Key: k, or the bearer token k. It rejects server/discover, answers initialize with a
// session in indented JSON, and once initialized lists the tool a as an event
// stream.
func fakeHTTPServer(t *testing.T) string {
	t.Helper()

	var initialized atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpReq *http.Request) {
		var req fakeRequest

		_ = json.NewDecoder(httpReq.Body).Decode(&req)

		switch {
		case httpReq.Header.Get("X-Key") != "k" && httpReq.Header.Get("Authorization") != "Bearer k":
			writer.WriteHeader(http.StatusUnauthorized)
		case req.Method == "initialize":
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("Mcp-Session-Id", "s1")
			// Indented, as a JSON answer may be.
			_, _ = fmt.Fprintf(writer,
				"{\n  \"jsonrpc\": \"2.0\",\n  \"id\": %s,\n  \"result\": {\"instructions\": \"Use fake.\"}\n}\n", req.ID)
		case httpReq.Header.Get("Mcp-Session-Id") != "s1", httpReq.Header.Get("Mcp-Protocol-Version") != "2025-11-25":
			writer.WriteHeader(http.StatusBadRequest)
		case req.Method == "notifications/initialized":
			initialized.Store(true)
			writer.WriteHeader(http.StatusAccepted)
		case req.Method == "tools/list" && initialized.Load():
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer,
				"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"tools\":[{\"name\":\"a\"}]}}\n\n", req.ID)
		}
	}))
	t.Cleanup(server.Close)

	return server.URL
}

// remoteServer adds the remote MCP server fake at url to Claude Code, sending
// the header X-Key: key.
func remoteServer(t *testing.T, machine *equiptest.Machine, url, key string) {
	t.Helper()
	writeJSON(t, claudeJSON(machine), map[string]any{"mcpServers": map[string]any{
		"fake": map[string]any{"type": "http", "url": url, "headers": map[string]string{"X-Key": key}},
	}})
}

func TestProbeSendsTheHTTPHeadersOfACodexRemoteServer(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, machine.CodexConfig(),
		"[mcp_servers.fake]\nurl = "+strconv.Quote(fakeHTTPServer(t))+"\nhttp_headers = { X-Key = \"k\" }\n")

	if got := probedRow(t, machine, repo); got.CostUnknown {
		t.Errorf("row = %+v, want its cost measured", got)
	}
}

func TestProbeSendsTheBearerTokenOfACodexRemoteServer(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Env = append(machine.Env, "FAKE_TOKEN=k")
	repo := machine.Repo("app")
	writeFile(t, machine.CodexConfig(),
		"[mcp_servers.fake]\nurl = "+strconv.Quote(fakeHTTPServer(t))+"\nbearer_token_env_var = \"FAKE_TOKEN\"\n")

	if got := probedRow(t, machine, repo); got.CostUnknown {
		t.Errorf("row = %+v, want its cost measured", got)
	}
}

func TestProbeSaysARemoteServerRefusedIt(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	remoteServer(t, machine, fakeHTTPServer(t), "wrong")

	err := newSession(t, machine, repo).ProbeCost("mcp:fake")()
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("ProbeCost = %v, want an error naming the 401", err)
	}
}

func TestProbeMeasuresARemoteServer(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	remoteServer(t, machine, fakeHTTPServer(t), "k")
	probedRow(t, machine, repo)

	// "Use fake." and the name mcp__fake__a: 21 bytes.
	if got := row(t, open(t, machine, repo), "fake"); got.CostUnknown || got.Cost != 7 {
		t.Errorf("row = %+v, want a cost of 7", got)
	}
}

// approvedProjectServer adds the .mcp.json server fake to repo, approved in
// its settings.local.json, in a folder Claude Code trusts when trusted says.
func approvedProjectServer(t *testing.T, machine *equiptest.Machine, repo string, trusted bool) {
	t.Helper()
	writeJSON(t, filepath.Join(repo, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{"fake": fakeConfig(t, "modern", "")},
	})
	writeJSON(t, settingsLocal(repo), map[string]any{"enabledMcpjsonServers": []string{"fake"}})
	writeJSON(t, claudeJSON(machine), map[string]any{
		"projects": map[string]any{repo: map[string]any{"hasTrustDialogAccepted": trusted}},
	})
}

func TestProbeMeasuresAProjectServerTheUserApproved(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	approvedProjectServer(t, machine, repo, true)

	if got := probedRow(t, machine, repo); got.CostUnknown {
		t.Errorf("row = %+v, want its cost measured", got)
	}
}

func TestProbeRefusesAProjectServerInAFolderClaudeCodeDoesNotTrust(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	approvedProjectServer(t, machine, repo, false)

	err := newSession(t, machine, repo).ProbeCost("mcp:fake")()
	if !errors.Is(err, equip.ErrCannotProbe) {
		t.Errorf("ProbeCost = %v, want ErrCannotProbe", err)
	}
}

func TestProbeRefusesAProjectServerTheRepoApprovesItself(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	approvedProjectServer(t, machine, repo, true)
	machine.RunGit(repo, "add", "-f", settingsLocal(repo))

	err := newSession(t, machine, repo).ProbeCost("mcp:fake")()
	if !errors.Is(err, equip.ErrCannotProbe) {
		t.Errorf("ProbeCost = %v, want ErrCannotProbe", err)
	}
}
