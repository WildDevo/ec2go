package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"ec2go/internal/awsx"
	"ec2go/internal/preflight"
	"ec2go/internal/tmux"
	"ec2go/internal/tui"
)

func main() {
	profileFlag := flag.String("profile", "", "AWS profile to use")
	regionFlag := flag.String("region", "", "AWS region to use")
	flag.Parse()

	if err := preflight.Check(exec.LookPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	profile := resolveProfile(*profileFlag)
	region := *regionFlag
	if region == "" {
		region = os.Getenv("AWS_REGION")
		if region == "" {
			region = os.Getenv("AWS_DEFAULT_REGION")
		}
	}

	ctx := context.Background()
	cfg, err := awsx.LoadConfig(ctx, profile, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load AWS config: %v\n", err)
		os.Exit(1)
	}

	if cfg.Region == "" {
		fmt.Fprintln(os.Stderr, "no AWS region configured")
		fmt.Fprintln(os.Stderr, "set one with: --region, AWS_REGION, or in ~/.aws/config for your profile")
		os.Exit(1)
	}

	p := tea.NewProgram(tui.New(cfg, profile), tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if m, ok := finalModel.(tui.Model); ok && m.TmuxSession != "" {
		if err := tmux.Attach(m.TmuxSession); err != nil {
			fmt.Fprintf(os.Stderr, "tmux attach: %v\n", err)
			os.Exit(1)
		}
	}
}

func resolveProfile(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("AWS_PROFILE"); v != "" {
		return v
	}

	profiles := awsx.ListProfiles()
	if len(profiles) == 0 {
		return ""
	}
	if len(profiles) == 1 {
		return profiles[0]
	}

	selected, err := pickProfile(profiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile selection: %v\n", err)
		os.Exit(1)
	}
	return selected
}

func pickProfile(profiles []string) (string, error) {
	m := newPickerModel(profiles)
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
	return pm.choice(), nil
}

type pickerModel struct {
	items     []string
	cursor    int
	cancelled bool
}

func newPickerModel(items []string) pickerModel {
	return pickerModel{items: items}
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
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

func (m pickerModel) View() string {
	s := "Select an AWS profile:\n\n"
	for i, item := range m.items {
		cursor := "  "
		if m.cursor == i {
			cursor = "> "
		}
		s += cursor + item + "\n"
	}
	s += "\n(j/k to move, enter to select, esc to cancel)\n"
	return s
}

func (m pickerModel) choice() string {
	return m.items[m.cursor]
}
