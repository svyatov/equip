// Command rows prints the rows equip lists for the Project in a directory, one
// per line, and the facet counts to stderr, to compare with what each agent
// loads. See docs/agents/checking-counts.md.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/svyatov/equip/internal/equip"
)

func main() {
	err := run(os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rows:", err)
		os.Exit(1) //nolint:forbidigo // main only
	}
}

// run writes the rows of the Project in args[0], or the working directory, to
// stdout as tab-separated kind, name, plugin, state, agents and Locations, and
// the facet counts to stderr.
func run(args []string, stdout, stderr io.Writer) error {
	machine, err := equip.MachineFromEnv()
	if err != nil {
		return fmt.Errorf("read the environment: %w", err)
	}

	dir := machine.WorkDir
	if len(args) > 0 {
		dir = args[0]
	}

	session, err := equip.Open(machine, dir)
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}

	view := session.View()
	for _, facet := range view.Facets {
		fmt.Fprintf(stderr, "%s\t%d\n", facet.Name, facet.Count())
	}

	for _, row := range view.Rows {
		detail := session.Detail(row.Key)

		agents := make([]string, 0, len(detail.Agents))
		for _, agent := range detail.Agents {
			agents = append(agents, agent.String())
		}

		locations := make([]string, 0, len(detail.Locations))
		for _, location := range detail.Locations {
			locations = append(locations, location.Agent.String()+": "+location.Path)
		}

		where := strings.Join(locations, " | ")
		fmt.Fprintf(stdout, "%s\t%s\t\t%s\t%s\t%s\n", row.Kind, row.Name, row.State, strings.Join(agents, ","), where)

		// A plugin's skills have no row, so each follows its plugin's line.
		for _, content := range detail.Contents {
			if content.Kind == equip.Skill {
				fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%s\n", content.Kind, content.Name, row.Name, content.State,
					strings.Join(agents, ","), where)
			}
		}
	}

	return nil
}
