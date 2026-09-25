package equip_test

import (
	"testing"

	"github.com/svyatov/equip/internal/equip"
)

func TestMachineFromEnvUsesXDGVariablesWithHomeDefaults(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("CODEX_HOME", "/codex")

	m, err := equip.MachineFromEnv()
	if err != nil {
		t.Fatal(err)
	}

	got := [...]string{m.Home, m.ConfigHome, m.StateHome, m.CacheHome, m.CodexHome}

	want := [...]string{"/home/u", "/cfg", "/home/u/.local/state", "/home/u/.cache", "/codex"}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
