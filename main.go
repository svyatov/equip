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

// errUnexpectedArg is the error of a positional argument, which equip takes
// none of.
var errUnexpectedArg = errors.New("unexpected argument")

func main() {
	err := run(os.Args[1:], os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "equip:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("equip", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	version := flags.Bool("version", false, "print the build version")

	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return printLine(stdout, usage)
	}

	if err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if flags.NArg() > 0 {
		return fmt.Errorf("%w %q\n\n%s", errUnexpectedArg, flags.Arg(0), usage)
	}

	if *version {
		info, _ := debug.ReadBuildInfo()

		return printLine(stdout, "equip", info.Main.Version)
	}

	machine, err := equip.MachineFromEnv()
	if err != nil {
		return err
	}

	session, err := equip.Open(machine, machine.WorkDir)
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(newTUI(session)).Run()
	if err != nil {
		return fmt.Errorf("run the TUI: %w", err)
	}

	return nil
}

// printLine writes a to w as fmt.Fprintln does.
func printLine(w io.Writer, a ...any) error {
	_, err := fmt.Fprintln(w, a...)
	if err != nil {
		return fmt.Errorf("print: %w", err)
	}

	return nil
}
