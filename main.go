// Command equip shows every extension Claude Code and Codex would load in a
// project and lets the user pick which ones are active there.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/svyatov/equip/internal/equip"
)

func main() {
	flag.CommandLine.SetOutput(os.Stdout)
	flag.Usage = func() {
		fmt.Println("Usage: equip [--help] [--version]\n\nOpens the extensions of the Project in the working directory.")
	}
	version := flag.Bool("version", false, "print the build version")
	flag.Parse()
	if *version {
		info, _ := debug.ReadBuildInfo()
		fmt.Println("equip", info.Main.Version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "equip:", err)
		os.Exit(1)
	}
}

func run() error {
	m, err := equip.MachineFromEnv()
	if err != nil {
		return err
	}
	s, err := equip.Open(m, m.WorkDir)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(&model{s: s}).Run()
	return err
}
