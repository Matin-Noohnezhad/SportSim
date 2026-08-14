// Command sportsim is the terminal frontend for the football management
// simulation.
//
//	sportsim              start a new career, or continue the most recent save
//	sportsim -load FILE   continue a specific save
//	sportsim -list        list saved careers
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"sportsim/assets"
	"sportsim/store"
	"sportsim/ui/tui"
)

func main() {
	load := flag.String("load", "", "path to a saved career")
	list := flag.Bool("list", false, "list saved careers and exit")
	cont := flag.Bool("continue", false, "resume the most recently saved career")
	flag.Parse()

	saves, _ := store.List(store.Dir())

	if *list {
		if len(saves) == 0 {
			fmt.Println("No saved careers in", store.Dir())
			return
		}
		fmt.Printf("Saved careers in %s:\n", store.Dir())
		for _, s := range saves {
			fmt.Printf("  %-28s %s  %.0f KB\n",
				s.Name, s.Saved.Format(time.DateTime), float64(s.Size)/1024)
		}
		return
	}

	path := *load
	if path == "" && *cont && len(saves) > 0 {
		path = saves[0].Path
	}

	m, err := tui.New(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sportsim:", err)
		os.Exit(1)
	}

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sportsim:", err)
		os.Exit(1)
	}
	_ = assets.Size
}
