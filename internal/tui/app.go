package tui

import (
	"context"
	"fmt"
	"sort"
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

const detailWidth = 40

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
		cfg:    cfg,
		state:  stateLoading,
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

func (m Model) showDetail() bool {
	return m.width > 100 && len(m.filtered) > 0
}

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("15"))
	normalStyle   = lipgloss.NewStyle()
	statusStyle   = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Padding(0, 1)
	detailBorder  = lipgloss.NewStyle().
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("238")).
			PaddingLeft(1).
			MarginLeft(1)
	detailLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	detailValue = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
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
	var top strings.Builder
	if m.filtering || m.filter.Value() != "" {
		top.WriteString(m.filter.View())
		top.WriteByte('\n')
	}

	listContent := m.renderListRows()

	var body string
	if m.showDetail() {
		detail := m.renderDetail()
		body = lipgloss.JoinHorizontal(lipgloss.Top, listContent, detail)
	} else {
		body = listContent
	}

	total := len(m.instances)
	showing := len(m.filtered)
	statusText := fmt.Sprintf("region=%s │ %d instances", m.cfg.Region, total)
	if showing != total {
		statusText = fmt.Sprintf("region=%s │ %d/%d instances", m.cfg.Region, showing, total)
	}

	return top.String() + body + "\n" + statusStyle.Render(statusText)
}

func (m Model) renderListRows() string {
	var b strings.Builder

	header := formatRow("NAME", "INSTANCE ID", "STATE", "PRIVATE IP", "SSM")
	b.WriteString(headerStyle.Render(header))
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("─", min(m.listWidth(), len(header)+4)))
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

	return b.String()
}

func (m Model) renderDetail() string {
	if m.cursor >= len(m.filtered) {
		return ""
	}
	inst := m.filtered[m.cursor]
	w := detailWidth - 4

	var b strings.Builder
	b.WriteString(detailValue.Bold(true).Render("Instance Detail"))
	b.WriteString("\n\n")

	writeField := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(detailLabel.Render(label))
		b.WriteString("  ")
		b.WriteString(detailValue.Render(truncate(value, w-len(label)-2)))
		b.WriteByte('\n')
	}

	writeField("ID", inst.ID)
	writeField("Name", inst.Name)
	writeField("State", inst.State)
	writeField("AZ", inst.AZ)
	writeField("AMI", inst.AMI)
	writeField("Private IP", inst.PrivateIP)
	writeField("Public IP", inst.PublicIP)
	writeField("SSM", inst.SSMStatus)

	if !inst.LaunchTime.IsZero() {
		writeField("Launched", inst.LaunchTime.Format("2006-01-02 15:04"))
	}

	if len(inst.Tags) > 0 {
		b.WriteString("\n")
		b.WriteString(detailLabel.Render("Tags"))
		b.WriteByte('\n')
		keys := make([]string, 0, len(inst.Tags))
		for k := range inst.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k == "Name" {
				continue
			}
			b.WriteString(detailLabel.Render("  "+k+"="))
			b.WriteString(detailValue.Render(truncate(inst.Tags[k], w-len(k)-4)))
			b.WriteByte('\n')
		}
	}

	return detailBorder.Width(detailWidth).Render(b.String())
}

func (m Model) listWidth() int {
	if m.showDetail() {
		return m.width - detailWidth - 4
	}
	return m.width
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
	if max <= 1 {
		return "…"
	}
	return s[:max-1] + "…"
}
