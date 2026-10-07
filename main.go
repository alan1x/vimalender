package main

import (
	"fmt"
	"os"

	"github.com/Sadoaz/vimalender/internal"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Non-interactive subcommand: check for upcoming events and notify.
	if len(os.Args) > 1 && os.Args[1] == "notify" {
		os.Exit(internal.RunNotify(os.Args[2:]))
	}

	p := tea.NewProgram(
		internal.NewModel(),
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
