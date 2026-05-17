package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/charmbracelet/bubbles/textinput"
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
	filtered  []awsx.Instance
	cursor    int
	offset    int
	filtering bool
	filter    textinput.Model
	err       error
	width     int
	height    int
}

func New(cfg aws.Config) Model {
	fi := textinput.New()
	fi.Prompt = "/ "
	fi.CharLimit = 128
	return Model{
		cfg:   cfg,
		state: stateLoading,
		filter: fi,
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
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case instancesMsg:
		if msg.err != nil {
			m.state = stateError
			m.err = msg.err
		} else {
			m.state = stateReady
			m.instances = msg.instances
			m.applyFilter()
		}
		return m, nil
	case tea.KeyMsg:
		if m.filtering {
			return m.updateFilter(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		return m, tea.Quit
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.applyFilter()
			return m, nil
		}
		return m, tea.Quit
	case "j", "down":
		if len(m.filtered) > 0 && m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		if len(m.filtered) > 0 {
			m.cursor = len(m.filtered) - 1
		}
	case "/":
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink
	}
	m.adjustOffset()
	return m, nil
}

func (m Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filter.SetValue("")
		m.filter.Blur()
		m.applyFilter()
		return m, nil
	case "enter":
		m.filtering = false
		m.filter.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.applyFilter()
	return m, cmd
}

func (m *Model) applyFilter() {
	query := strings.ToLower(m.filter.Value())
	if query == "" {
		m.filtered = m.instances
	} else {
		m.filtered = nil
		for _, inst := range m.instances {
			if matchInstance(inst, query) {
				m.filtered = append(m.filtered, inst)
			}
		}
	}
	m.cursor = 0
	m.offset = 0
}

func matchInstance(inst awsx.Instance, query string) bool {
	if strings.Contains(strings.ToLower(inst.Name), query) {
		return true
	}
	if strings.Contains(strings.ToLower(inst.ID), query) {
		return true
	}
	if strings.Contains(strings.ToLower(inst.PrivateIP), query) {
		return true
	}
	for _, v := range inst.Tags {
		if strings.Contains(strings.ToLower(v), query) {
			return true
		}
	}
	return false
}

func (m *Model) adjustOffset() {
	visible := m.visibleRows()
	if visible <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
}

func (m Model) visibleRows() int {
	// header(1) + separator(1) + status bar(1) + optional filter(1)
	chrome := 3
	if m.filtering || m.filter.Value() != "" {
		chrome = 4
	}
	rows := m.height - chrome
	if rows < 1 {
		return 1
	}
	return rows
}

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("15"))
	normalStyle = lipgloss.NewStyle()
	statusStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Padding(0, 1)
)

const (
	colName  = 20
	colID    = 21
	colState = 10
	colIP    = 16
	colSSM   = 8
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

	if m.filtering || m.filter.Value() != "" {
		b.WriteString(m.filter.View())
		b.WriteByte('\n')
	}

	header := formatRow("NAME", "INSTANCE ID", "STATE", "PRIVATE IP", "SSM")
	b.WriteString(headerStyle.Render(header))
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("─", min(m.width, len(header)+4)))
	b.WriteByte('\n')

	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	if len(m.filtered) == 0 {
		b.WriteString("  no matches\n")
	}

	for idx := m.offset; idx < end; idx++ {
		inst := m.filtered[idx]
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

	for i := end - m.offset; i < visible; i++ {
		b.WriteByte('\n')
	}

	total := len(m.instances)
	showing := len(m.filtered)
	statusText := fmt.Sprintf("region=%s │ %d instances", m.cfg.Region, total)
	if showing != total {
		statusText = fmt.Sprintf("region=%s │ %d/%d instances", m.cfg.Region, showing, total)
	}
	b.WriteString(statusStyle.Render(statusText))

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
