package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ec2go/internal/awsx"
)

type state int

const (
	stateLoading state = iota
	stateReady
	stateError
)

type instancesMsg struct {
	instances []awsx.Instance
	err       error
}

type Model struct {
	cfg       aws.Config
	state     state
	instances []awsx.Instance
	cursor    int
	offset    int
	err       error
	width     int
	height    int
}

func New(cfg aws.Config) Model {
	return Model{
		cfg:   cfg,
		state: stateLoading,
	}
}

func (m Model) Init() tea.Cmd {
	return m.fetchInstances
}

func (m Model) fetchInstances() tea.Msg {
	ctx := context.Background()
	instances, err := awsx.ListInstances(ctx, m.cfg)
	if err != nil {
		return instancesMsg{err: err}
	}
	status, err := awsx.PingStatus(ctx, m.cfg)
	if err != nil {
		return instancesMsg{err: err}
	}
	instances = awsx.MergeSSMStatus(instances, status)
	return instancesMsg{instances: instances}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if len(m.instances) > 0 && m.cursor < len(m.instances)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			if len(m.instances) > 0 {
				m.cursor = len(m.instances) - 1
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case instancesMsg:
		if msg.err != nil {
			m.state = stateError
			m.err = msg.err
		} else {
			m.state = stateReady
			m.instances = msg.instances
		}
	}
	visible := m.visibleRows()
	if visible > 0 {
		if m.cursor < m.offset {
			m.offset = m.cursor
		}
		if m.cursor >= m.offset+visible {
			m.offset = m.cursor - visible + 1
		}
	}
	return m, nil
}

func (m Model) visibleRows() int {
	// height minus header(1) + separator(1) + status bar(1)
	rows := m.height - 3
	if rows < 1 {
		return 1
	}
	return rows
}

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("15"))
	normalStyle   = lipgloss.NewStyle()
	statusStyle   = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Padding(0, 1)
)

const (
	colName = 20
	colID   = 21
	colState = 10
	colIP   = 16
	colSSM  = 8
)

func (m Model) View() string {
	switch m.state {
	case stateLoading:
		return "Loading instances...\n"
	case stateError:
		return fmt.Sprintf("Error: %v\n\nPress q to quit.\n", m.err)
	default:
		return m.viewList()
	}
}

func (m Model) viewList() string {
	var b strings.Builder

	header := formatRow("NAME", "INSTANCE ID", "STATE", "PRIVATE IP", "SSM")
	b.WriteString(headerStyle.Render(header))
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("─", min(m.width, len(header)+4)))
	b.WriteByte('\n')

	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.instances) {
		end = len(m.instances)
	}

	for idx := m.offset; idx < end; idx++ {
		inst := m.instances[idx]
		row := formatRow(
			truncate(inst.Name, colName),
			inst.ID,
			inst.State,
			inst.PrivateIP,
			inst.SSMStatus,
		)
		if idx == m.cursor {
			b.WriteString(selectedStyle.Render(row))
		} else {
			b.WriteString(normalStyle.Render(row))
		}
		b.WriteByte('\n')
	}

	// pad empty lines to keep status bar at bottom
	for i := end - m.offset; i < visible; i++ {
		b.WriteByte('\n')
	}

	status := statusStyle.Render(
		fmt.Sprintf("region=%s │ %d instances", m.cfg.Region, len(m.instances)),
	)
	b.WriteString(status)

	return b.String()
}

func formatRow(name, id, state, ip, ssm string) string {
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s",
		colName, name,
		colID, id,
		colState, state,
		colIP, ip,
		colSSM, ssm,
	)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
