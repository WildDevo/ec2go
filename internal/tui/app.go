package tui

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ec2go/internal/awsx"
	"ec2go/internal/connect"
	"ec2go/internal/tmux"
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

type sessionDoneMsg struct{ err error }

type Model struct {
	cfg          aws.Config
	profile      string
	state        state
	instances    []awsx.Instance
	filtered     []awsx.Instance
	selected     map[string]bool
	cursor       int
	offset       int
	filtering    bool
	filter       textinput.Model
	err          error
	width        int
	height       int
	TmuxSession  string
}

func New(cfg aws.Config, profile string) Model {
	fi := textinput.New()
	fi.Prompt = "/ "
	fi.CharLimit = 128
	return Model{
		cfg:      cfg,
		profile:  profile,
		state:    stateLoading,
		filter:   fi,
		selected: make(map[string]bool),
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
	case sessionDoneMsg:
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
	case " ", "tab":
		if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
			id := m.filtered[m.cursor].ID
			if m.selected[id] {
				delete(m.selected, id)
			} else {
				m.selected[id] = true
			}
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		}
	case "enter":
		return m.connectSelected()
	case "/":
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink
	}
	m.adjustOffset()
	return m, nil
}

func (m Model) connectSelected() (tea.Model, tea.Cmd) {
	targets := m.selectedInstances()
	if len(targets) == 0 {
		return m, nil
	}
	if len(targets) == 1 {
		inst := targets[0]
		args := connect.BuildSSMArgs(inst.ID, m.cfg.Region, m.profile)
		cmd := exec.Command("aws", args...)
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return sessionDoneMsg{err: err}
		})
	}
	var panes []tmux.Pane
	for _, inst := range targets {
		args := connect.BuildSSMArgs(inst.ID, m.cfg.Region, m.profile)
		panes = append(panes, tmux.Pane{
			Title:   fmt.Sprintf("%s | %s", inst.Name, inst.ID),
			Command: "aws " + strings.Join(args, " "),
		})
	}
	session, err := tmux.Setup(panes)
	if err != nil {
		m.err = fmt.Errorf("tmux setup: %w", err)
		return m, nil
	}
	m.TmuxSession = session
	return m, tea.Quit
}

func (m Model) selectedInstances() []awsx.Instance {
	if len(m.selected) == 0 {
		if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
			return []awsx.Instance{m.filtered[m.cursor]}
		}
		return nil
	}
	var out []awsx.Instance
	for _, inst := range m.filtered {
		if m.selected[inst.ID] {
			out = append(out, inst)
		}
	}
	return out
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
	chrome := 5
	if m.filtering || m.filter.Value() != "" {
		chrome = 6
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
	titleStyle    = lipgloss.NewStyle().Background(lipgloss.Color("208")).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	titleCtxStyle = lipgloss.NewStyle().Background(lipgloss.Color("208")).Foreground(lipgloss.Color("233")).Padding(0, 1)
	titleFill     = lipgloss.NewStyle().Background(lipgloss.Color("208"))
	statusStyle   = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Padding(0, 1)
	detailBorder  = lipgloss.NewStyle().
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("238")).
			PaddingLeft(1).
			MarginLeft(1)
	detailLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	detailValue = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

	stateRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stateWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	stateStopped = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	ssmOnline  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	ssmOffline = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

const (
	colName  = 20
	colID    = 21
	colState = 10
	colIP    = 16
	colSSM   = 8
)

func (m Model) View() string {
	var out strings.Builder
	out.WriteString(m.renderTitle())
	out.WriteByte('\n')

	switch m.state {
	case stateLoading:
		out.WriteString("Loading instances...\n")
	case stateError:
		fmt.Fprintf(&out, "Error: %v\n\nPress q to quit.\n", m.err)
	default:
		out.WriteString(m.viewReady())
	}
	return out.String()
}

func (m Model) renderTitle() string {
	left := titleStyle.Render("ec2go")
	ctx := m.cfg.Region
	if m.profile != "" {
		ctx = m.profile + "/" + ctx
	}
	right := titleCtxStyle.Render(ctx)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	fill := titleFill.Render(strings.Repeat(" ", gap))
	return left + fill + right
}

func (m Model) viewReady() string {
	var out strings.Builder

	if m.filtering || m.filter.Value() != "" {
		out.WriteString(m.filter.View())
		out.WriteByte('\n')
	}

	listContent := m.renderListRows()

	var body string
	if m.showDetail() {
		detail := m.renderDetail()
		body = lipgloss.JoinHorizontal(lipgloss.Top, listContent, detail)
	} else {
		body = listContent
	}
	out.WriteString(body)

	total := len(m.instances)
	showing := len(m.filtered)
	sel := len(m.selected)
	statusText := fmt.Sprintf("%d instances", total)
	if showing != total {
		statusText = fmt.Sprintf("%d/%d instances", showing, total)
	}
	if sel > 0 {
		statusText += fmt.Sprintf(" │ %d selected", sel)
	}
	if m.err != nil {
		statusText += fmt.Sprintf(" │ %v", m.err)
	}

	out.WriteByte('\n')
	out.WriteString(statusStyle.Render(statusText))
	return out.String()
}

func (m Model) renderListRows() string {
	var b strings.Builder

	header := formatRow("NAME", "INSTANCE ID", "STATE", "PRIVATE IP", "SSM", false)
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
		marker := "  "
		if m.selected[inst.ID] {
			marker = "* "
		}
		isCursor := idx == m.cursor
		row := marker + formatRow(
			truncate(inst.Name, colName),
			inst.ID,
			inst.State,
			inst.PrivateIP,
			inst.SSMStatus,
			!isCursor,
		)
		if isCursor {
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

	writeStyledField := func(label, raw string, styled string) {
		if raw == "" {
			return
		}
		b.WriteString(detailLabel.Render(label))
		b.WriteString("  ")
		b.WriteString(styled)
		b.WriteByte('\n')
	}

	writeField("ID", inst.ID)
	writeField("Name", inst.Name)
	writeStyledField("State", inst.State, colorState(inst.State))
	writeField("AZ", inst.AZ)
	writeField("AMI", inst.AMI)
	writeField("Private IP", inst.PrivateIP)
	writeField("Public IP", inst.PublicIP)
	writeStyledField("SSM", inst.SSMStatus, colorSSM(inst.SSMStatus))

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

func formatRow(name, id, state, ip, ssm string, colorize bool) string {
	if colorize {
		return pad(name, colName) + "  " +
			pad(id, colID) + "  " +
			colorState(pad(state, colState)) + "  " +
			pad(ip, colIP) + "  " +
			colorSSM(pad(ssm, colSSM))
	}
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s",
		colName, name,
		colID, id,
		colState, state,
		colIP, ip,
		colSSM, ssm,
	)
}

func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func colorState(s string) string {
	switch strings.TrimSpace(s) {
	case "running":
		return stateRunning.Render(s)
	case "pending", "stopping", "shutting-down":
		return stateWarning.Render(s)
	case "stopped", "terminated":
		return stateStopped.Render(s)
	default:
		return s
	}
}

func colorSSM(s string) string {
	switch strings.TrimSpace(s) {
	case "Online":
		return ssmOnline.Render(s)
	case "":
		return s
	default:
		return ssmOffline.Render(s)
	}
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
