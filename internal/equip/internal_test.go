package equip

import (
	"strings"
	"testing"
)

func TestFieldReadsYAMLStrings(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ name, value, want string }{
		{"literal block", " |\n  Reviews code.\n  Use before a merge.", "Reviews code.\nUse before a merge."},
		{"folded block", " >-\n  Reviews code.\n  Use before a merge.", "Reviews code. Use before a merge."},
		{"plain value on the next lines", "\n  Reviews code.\n  Use before a merge.", "Reviews code. Use before a merge."},
		{"double quoted", ` "Reviews \"code\": use before a merge."`, `Reviews "code": use before a merge.`},
		{"single quoted", ` 'Reviews the user''s code.'`, "Reviews the user's code."},
	} {
		skill := "---\nname: review\ndescription:" + testCase.value + "\nlicense: MIT\n---\nBody.\n"
		if got := field([]byte(skill), "description"); got != testCase.want {
			t.Errorf("%s: description = %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

func TestSkillCost(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, description, whenToUse string
		agent                        Agent
		want                         int
	}{
		// 3 + 12 + 15 = 30 bytes.
		{"Claude Code counts when_to_use", strings.Repeat("x", 12), strings.Repeat("y", 15), ClaudeCode, 10},
		// "é" is 2 bytes: 3 + 1,536 × 2 = 3,075 bytes.
		{"Claude Code caps the text", strings.Repeat("é", 1000), strings.Repeat("é", 600), ClaudeCode, 1025},
		// 3 + 13 = 16 bytes.
		{"Codex leaves out when_to_use", strings.Repeat("x", 13), strings.Repeat("y", 50), Codex, 4},
		// 3 + 1,024 × 2 = 2,051 bytes, rounded up.
		{"Codex caps the description", strings.Repeat("é", 1200), "", Codex, 513},
	} {
		skill := "---\nname: abc\ndescription: " + testCase.description + "\nwhen_to_use: " + testCase.whenToUse + "\n---\n"
		if got := skillCost(testCase.agent, t.TempDir(), "abc", []byte(skill)); got != testCase.want {
			t.Errorf("%s: cost = %d, want %d", testCase.name, got, testCase.want)
		}
	}
}

func TestClaudeCodeCostsTheFirst2048CharactersOfInstructions(t *testing.T) {
	t.Parallel()

	tool := mcpTool{Name: "a", Description: "", InputSchema: nil}
	server := measurement{TTLMs: nil, Instructions: strings.Repeat("é", 3000), Tools: []mcpTool{tool}}

	// 2048 two-byte characters and mcp__fake__a: 4108 bytes.
	if got := server.cost(ClaudeCode, "fake", false); got != 1370 {
		t.Errorf("cost = %d, want 1370", got)
	}
}
