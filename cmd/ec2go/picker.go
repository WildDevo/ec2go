package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

func pickProfile(profiles []string) (string, error) {
	m := pickerModel{items: profiles}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	pm := final.(pickerModel)
	if pm.cancelled {
		fmt.Fprintln(os.Stderr, "no profile selected")
		os.Exit(0)
	}
	return pm.items[pm.cursor], nil
}

type pickerModel struct {
	items     []string
	cursor    int
	cancelled bool
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m pickerModel) View() tea.View {
	s := "Select an AWS profile:\n\n"
	for i, item := range m.items {
		cursor := "  "
		if m.cursor == i {
			cursor = "> "
		}
		s += cursor + item + "\n"
	}
	s += "\n(j/k to move, enter to select, esc to cancel)\n"
	return tea.NewView(s)
}
