// Command equip shows every extension Claude Code and Codex would load in a
// project and lets the user pick which ones are active there.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/svyatov/equip/internal/equip"
)

const usage = "Usage: equip [--help] [--version]\n\nOpens the extensions of the Project in the working directory."

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "equip:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("equip", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.Bool("version", false, "print the build version")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		_, err = fmt.Fprintln(stdout, usage)
		return err
	} else if err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q\n\n%s", fs.Arg(0), usage)
	}
	if *version {
		info, _ := debug.ReadBuildInfo()
		_, err := fmt.Fprintln(stdout, "equip", info.Main.Version)
		return err
	}
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
