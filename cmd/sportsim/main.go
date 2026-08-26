// Command sportsim is the terminal frontend for the football management
// simulation.
//
//	sportsim              start a new career, or continue the most recent save
//	sportsim -load FILE   continue a specific save
//	sportsim -list        list saved careers
//	sportsim -editions    list the seasons a career can start in
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"sportsim/assets"
	"sportsim/engine/model"
	"sportsim/game"
	"sportsim/store"
	"sportsim/ui/tui"
)

func main() {
	load := flag.String("load", "", "path to a saved career")
	list := flag.Bool("list", false, "list saved careers and exit")
	editions := flag.Bool("editions", false, "list the seasons a career can start in and exit")
	cont := flag.Bool("continue", false, "resume the most recently saved career")
	flag.Parse()

	if *editions {
		years := game.Editions()
		if len(years) == 0 {
			fmt.Println("No player databases are embedded in this build.")
			return
		}
		fmt.Printf("Seasons available (%.1f MB embedded):\n", float64(assets.Size())/(1024*1024))
		for _, y := range years {
			fmt.Printf("  %s\n", model.SeasonLabel(y))
		}
		return
	}

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
}
