package data

import (
	"cmp"
	"slices"
	"strings"
)

const (
	userClaude = "~/.claude/skills"
	userCodex  = "~/.codex/skills"
	userShared = "~/.agents/skills"
	project    = ".claude/skills"
)

func preset(name, members string) Preset {
	p := Preset{Name: name, Members: map[string]bool{}}
	for m := range strings.FieldsSeq(members) {
		p.Members[m] = true
	}
	return p
}

// the user's presets; only go is active in this project.
var library = []Preset{
	preset("go", "tdd grilling domain-modeling research prototype code-review diagnosing-bugs codebase-design "+
		"writing-for-agents modern-go-guidelines atomic-commits ponytail dependency-vetting codegraph github wayfinder go-release"),
	preset("ruby", "tdd grilling research code-review diagnosing-bugs rails-upgrade rspec-fix sql-review "+
		"atomic-commits dependency-vetting github postgres superpowers"),
	preset("writing", "brain spiral research grilling writing-for-agents oss-kit claude-in-chrome"),
	preset("accounting", "brain spiral notion postgres ledger sandbox-run"),
}

var overrides = map[string]State{
	"compound-engineering": On, "brain": Manual, "spiral": On, "wizard": Manual,
	"ponytail": Off, "oss-tracker": Manual,
}

// descriptions of plugin contents, lengthy on purpose: real ones are.
var childDesc = map[string]string{
	"ce-babysit-pr":          "Babysits an open GitHub PR until merge-ready. Use when asked to watch a PR over time, not for one-shot comment resolution or one CI failure. GitHub (incl. Enterprise) only.",
	"ce-bakeoff":             "Develop independent competing solutions to a defined brief, compare them, and synthesize a winning approach. Use when choosing well requires developing alternatives beyond their current form.",
	"ce-brainstorm":          "Explore vague or ambitious ideas into a right-sized requirements-only unified plan. Use when the user wants to brainstorm or scope what to build. Not for executing already-specified work.",
	"ce-code-review":         "Review a named diff or PR for bugs, regressions, tests, and standards. Use when asked to review code or when a shipping skill needs a review receipt.",
	"ce-commit":              "Create a git commit with a clear, value-communicating message. Use when the user asks to commit or save staged or unstaged changes with a repo-appropriate message.",
	"ce-commit-push-pr":      "Commit, push, and open a PR. Use when asked to ship or open a PR, or for PR-description-only flows like writing, rewriting, or describing a PR body.",
	"ce-compound":            "Document a solved problem as a durable repo learning. Use when verified work produced non-obvious reasoning absent from its final code, tests, or existing docs.",
	"ce-compound-refresh":    "Refresh the repo's captured learnings against the current codebase. Use when auditing stale, overlapping, superseded, or drifted learnings.",
	"ce-debug":               "Diagnosis loop for bugs and failing behavior. Use when asked to debug or fix failing or slow behavior.",
	"ce-doc-review":          "Review requirements, plans, or specs with role-specific lenses. Use when the user wants to improve an existing planning document.",
	"ce-explain":             "Explain how and why something has its current shape, or what happened over a window of work, grounded in evidence. Use when the user asks for an explanation.",
	"ce-handoff":             "Create a session handoff for another agent, or resume, find, and read any user-selected continuity source.",
	"ce-ideate":              "Generate and evaluate grounded ideas. Use when the user wants ideas, improvements, or surprising directions before choosing one to develop.",
	"ce-noslop":              "Rewrite, check, or draft prose so it carries no AI writing tells, reads plainly on the first read, and keeps every source fact.",
	"ce-optimize":            "Optimize a named target with a measured loop: attribute a workload's cost, or score variants and keep winners.",
	"ce-plan":                "Create structured plans for multi-step work, including software and non-software tasks. Use when asked to plan, break down implementation, or deepen an existing plan.",
	"ce-pov":                 "Judge a supplied subject against the project's evidence and constraints. Use when assessing an external-adoption question or a holistic take on a document.",
	"ce-proof":               "Publish, read, comment on, or edit markdown in Proof. Use for Proof links, sharing specs, plans, and drafts, or publish handoffs from planning workflows.",
	"ce-prototype":           "Build a throwaway prototype to answer how something should work, feel, or read. Use when committing the wrong answer would be expensive to unravel.",
	"ce-resolve-pr-feedback": "Resolve PR review feedback. Use when addressing feedback already left on a PR.",
	"ce-simplify-code":       "Simplify settled, recently changed code for clarity, reuse, quality, and efficiency while preserving behavior.",
	"ce-strategy":            "Create or update STRATEGY.md. Use when starting a product, adding a strategy doc, or changing direction or roadmap.",
	"ce-test-browser":        "Run browser tests for pages affected by the current branch or PR.",
	"ce-work":                "Execute a plan or concrete work prompt end-to-end. Use when implementing from a plan document, a spec path, or a clear build request.",
	"ce-worktree":            "Set up isolated git worktrees: create a new branch for fresh work, or attach a worktree to an existing branch, PR, or commit.",
	"lfg":                    "Take a request all the way to done, hands-off, through the right skills. A code change ends as an open pull request, pushed without stopping.",
	"oss-audit":              "Score an open source repository against the oss-kit standard and report what is missing: docs, community files, CI, security posture, release process.",
	"oss-changelog":          "Maintain a changelog and make versioning decisions: Keep a Changelog structure, semantic version choices, release notes, deprecation policy.",
	"oss-ci":                 "Set up continuous integration on GitHub Actions or GitLab CI/CD: tests, builds and linting on push and pull requests.",
	"oss-community":          "Create CONTRIBUTING, CODE_OF_CONDUCT, SECURITY.md, issue forms, PR templates, CODEOWNERS and the license file.",
	"oss-harden":             "Harden a repository's security posture: pin actions, restrict workflow permissions, enable dependency updates, branch protection, signed tags.",
	"oss-publish":            "Set up a secure release process with trusted publishing, OIDC, build provenance and approval-gated release workflows.",
	"oss-readme":             "Write or improve a README.md for an open source project.",
	"oss-skill":              "Fix the structure, portability, and effectiveness of repositories that ship Agent Skills.",
	"oss-writing":            "Write clear technical prose for commits, PRs, issues, READMEs, docs and changelogs.",
}

// plugin contents set by hand in this project.
var childOverrides = map[string]State{
	"ce-strategy": Off, "ce-proof": Off, "ce-test-browser": Off, "lfg": Manual,
	"ce-babysit-pr": Manual, "oss-publish": Manual,
}

// Fake returns a store shaped like a real two-agent setup.
func Fake() *Store {
	s := &Store{Project: "~/Projects/My/open-source/equip", Presets: []string{"go"}, Library: library}
	skill := func(name, src string, a Agent, desc string) {
		s.Exts = append(s.Exts, &Ext{Kind: Skill, Name: name, Source: src, Agents: a, Desc: desc, Tokens: tok(name, 40, 220)})
	}
	for _, x := range []struct{ name, desc string }{
		{"brain", "Reads and writes the personal knowledge base at ~/Projects/Brain"},
		{"code-review", "Review changes since a fixed point along standards, spec and adversarial axes"},
		{"codebase-design", "Shared vocabulary for designing deep modules"},
		{"diagnosing-bugs", "Diagnosis loop for hard bugs and performance regressions"},
		{"domain-modeling", "Build and sharpen a project's domain model and glossary"},
		{"grilling", "Grill the user relentlessly about a plan, decision, or idea"},
		{"oss-tracker", "Sweep every owned repository and report what needs a decision"},
		{"pr", "The shape of a pull request body: diagram, evidence, merge danger"},
		{"prototype", "Build a throwaway prototype to answer a design question"},
		{"research", "Investigate a question against high-trust primary sources"},
		{"resolving-merge-conflicts", "Resolve an in-progress merge or rebase conflict"},
		{"tdd", "Test-driven development with red-green-refactor"},
		{"track-contrib", "Track and review open-source contributions"},
		{"wizard", "Generate an interactive bash wizard for human-only steps"},
		{"writing-for-agents", "Writing documents that agents read"},
		{"wayfinder", "Chart a foggy effort as a shared map of decision tickets"},
		{"to-spec", "Collapse a map's decisions into a buildable plan"},
		{"setup-supermatt-skills", "Configure issue tracker and labels for agent skills"},
		{"triage", "Triage incoming issues with the label vocabulary"},
		{"changelog-entry", "Draft a Keep a Changelog entry from merged work"},
	} {
		skill(x.name, userClaude, Claude, x.desc)
	}
	for _, x := range []struct{ name, desc string }{
		{"commit", "Write a Conventional Commits message for staged changes"},
		{"explain-diff", "Explain a diff in plain language"},
		{"sql-review", "Review SQL migrations for locking and data loss"},
		{"rails-upgrade", "Step through a Rails version upgrade"},
		{"rspec-fix", "Fix failing RSpec examples one at a time"},
		{"docker-slim", "Shrink a Dockerfile's image size"},
		{"k8s-debug", "Debug a failing Kubernetes deployment"},
		{"terraform-plan", "Read a terraform plan and flag risky changes"},
		{"api-client", "Generate a typed API client from an OpenAPI spec"},
		{"regex-explain", "Explain and test a regular expression"},
		{"perf-profile", "Profile a Go or Ruby program and find hot paths"},
		{"security-scan", "Scan a diff for secrets and injection risks"},
	} {
		skill(x.name, userShared, Both, x.desc)
	}
	for _, x := range []struct{ name, desc string }{
		{"codex-review", "Codex-native review of the working tree"},
		{"sandbox-run", "Run a command in the Codex sandbox and report"},
		{"plan-mode", "Write a plan before editing files"},
		{"ledger", "Keep a running log of decisions in the session"},
	} {
		skill(x.name, userCodex, Codex, x.desc)
	}
	skill("go-release", project, Claude, "Cut a release of this Go module with goreleaser")
	skill("golden-update", project, Claude, "Regenerate golden files for TUI snapshot tests")
	skill("fixtures", project, Claude, "Build fake agent config trees for tests")

	plugin := func(name, market string, a Agent, desc string, skills []string, mcps ...string) {
		p := &Ext{Kind: Plugin, Name: name, Source: market, Agents: a, Desc: desc}
		for _, n := range skills {
			p.Children = append(p.Children, &Ext{Kind: Skill, Name: n, Source: name, Agents: a, Tokens: tok(n, 40, 200), Parent: p, State: On, Origin: "default",
				Desc: cmp.Or(childDesc[n], "The "+n+" skill from the "+name+" plugin.")})
		}
		for _, n := range mcps {
			p.Children = append(p.Children, &Ext{Kind: MCP, Name: n, Source: name, Agents: a, Tokens: tok(n, 1500, 9000), Parent: p, State: On, Origin: "default",
				Desc: "MCP tools for the " + name + " plugin: " + desc})
		}
		slices.SortFunc(p.Children, func(a, b *Ext) int { return strings.Compare(a.Name, b.Name) })
		for _, c := range p.Children {
			p.Tokens += c.Tokens
		}
		s.Exts = append(s.Exts, p)
	}
	plugin("compound-engineering", "every-marketplace", Claude, "Plan, work, review, compound: a full engineering loop",
		strings.Fields("ce-brainstorm ce-plan ce-work ce-code-review ce-debug ce-commit ce-commit-push-pr ce-compound ce-compound-refresh ce-doc-review ce-explain ce-handoff ce-ideate ce-noslop ce-optimize ce-pov ce-proof ce-prototype ce-resolve-pr-feedback ce-simplify-code ce-strategy ce-test-browser ce-worktree ce-babysit-pr ce-bakeoff lfg"))
	plugin("oss-kit", "svyatov-agent-toolkit", Both, "Open source project hygiene: CI, docs, releases, security",
		strings.Fields("oss-audit oss-changelog oss-ci oss-community oss-harden oss-publish oss-readme oss-skill oss-writing"))
	plugin("ponytail", "ponytail", Claude, "Force the laziest solution that works",
		strings.Fields("ponytail ponytail-audit ponytail-debt ponytail-gain ponytail-help ponytail-review"))
	plugin("chrome-devtools-mcp", "claude-plugins-official", Claude, "Chrome DevTools for debugging and automation",
		strings.Fields("a11y-debugging chrome-devtools chrome-devtools-cli cookie-debugging debug-optimize-lcp memory-leak-debugging troubleshooting"), "chrome-devtools")
	plugin("claude-md-management", "claude-plugins-official", Claude, "Audit and improve CLAUDE.md files",
		strings.Fields("revise-claude-md claude-md-improver"))
	plugin("atomic-commits", "svyatov-agent-toolkit", Both, "Commit early and often in atomic increments", []string{"atomic-commits"})
	plugin("dependency-vetting", "svyatov-agent-toolkit", Both, "Verify a package is authentic before installing", []string{"dependency-vetting"})
	plugin("handrail", "svyatov-agent-toolkit", Claude, "Turn guardrails into enforced rules", []string{"add", "analyze"})
	plugin("modern-go-guidelines", "goland-claude-marketplace", Both, "Version-aware modern Go idioms", []string{"use-modern-go"})
	plugin("playwright", "claude-plugins-official", Claude, "Browser automation with Playwright", []string{"playwright-test"}, "playwright")
	plugin("sentry", "claude-plugins-official", Claude, "Query Sentry issues and traces", []string{"sentry-triage"}, "sentry")
	plugin("supabase", "claude-plugins-official", Claude, "Manage Supabase projects", nil, "supabase")
	plugin("stripe", "claude-plugins-official", Claude, "Stripe API helpers and docs", []string{"stripe-best-practices"}, "stripe")
	plugin("vercel", "claude-plugins-official", Claude, "Deploy and inspect Vercel projects", []string{"vercel-deploy", "vercel-logs"})
	plugin("figma", "claude-plugins-official", Claude, "Read Figma designs into code", []string{"figma-to-code"}, "figma")
	plugin("rust-analyzer-lsp", "claude-plugins-official", Claude, "Rust language server", nil)
	plugin("gopls-lsp", "claude-plugins-official", Claude, "Go language server", nil)
	plugin("linear", "openai-curated", Codex, "Linear issues from Codex", []string{"linear-triage"}, "linear")
	plugin("notion", "openai-curated", Codex, "Notion pages as context", nil, "notion")
	plugin("superpowers", "superpowers-marketplace", Both, "Brainstorm, plan, execute with subagents",
		strings.Fields("brainstorming writing-plans executing-plans subagent-driven-development systematic-debugging verification-before-completion"))

	mcp := func(name, src string, a Agent, desc string) {
		s.Exts = append(s.Exts, &Ext{Kind: MCP, Name: name, Source: src, Agents: a, Desc: desc, Tokens: tok(name, 1200, 14000)})
	}
	mcp("codegraph", "~/.claude.json", Both, "Knowledge graph of the codebase's symbols and call paths")
	mcp("github", "~/.claude.json", Both, "GitHub issues, PRs and code search")
	mcp("spiral", "~/.claude.json", Claude, "Writes prose in the user's voice")
	mcp("claude-in-chrome", "built-in", Claude, "Drive the user's Chrome browser")
	mcp("postgres", "~/.codex/config.toml", Codex, "Read-only access to a local Postgres")

	for _, e := range s.Exts {
		e.State, e.Origin = Off, "default"
		if library[0].Members[e.Name] {
			e.State, e.Origin = On, "preset go"
		}
		if st, ok := overrides[e.Name]; ok {
			e.State, e.Origin = st, "override"
		}
	}
	for _, e := range s.All() {
		if st, ok := childOverrides[e.Name]; ok && e.Parent != nil {
			e.State, e.Origin = st, "override"
		}
	}
	s.snapshot()
	return s
}
