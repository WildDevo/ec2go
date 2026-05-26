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
	stateLoading      state = iota
	stateReady
	stateError
	stateRegionPicker
)

const (
	minDetailWidth = 40
	maxDetailWidth = 70
)

type instancesMsg struct {
	instances []awsx.Instance
	err       error
}

type sessionDoneMsg struct{ err error }

var awsRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"af-south-1",
	"ap-east-1",
	"ap-south-1",
	"ap-south-2",
	"ap-southeast-1",
	"ap-southeast-2",
	"ap-southeast-3",
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-northeast-3",
	"ca-central-1",
	"eu-central-1",
	"eu-central-2",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"eu-south-1",
	"eu-south-2",
	"eu-north-1",
	"il-central-1",
	"me-south-1",
	"me-central-1",
	"sa-east-1",
}

type Model struct {
	cfg            aws.Config
	profile        string
	state          state
	prevState      state
	instances      []awsx.Instance
	filtered       []awsx.Instance
	selected       map[string]bool
	cursor         int
	offset         int
	filtering      bool
	filter         textinput.Model
	err            error
	width          int
	height         int
	TmuxSession    string
	regionCursor   int
	regionOffset   int
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
		if m.state == stateRegionPicker {
			return m.updateRegionPicker(msg)
		}
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
	case "r":
		if m.state == stateReady {
			m.prevState = m.state
			m.state = stateRegionPicker
			m.regionCursor = 0
			m.regionOffset = 0
			for i, r := range awsRegions {
				if r == m.cfg.Region {
					m.regionCursor = i
					break
				}
			}
			return m, nil
		}
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

func (m Model) updateRegionPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "r":
		m.state = m.prevState
		return m, nil
	case "j", "down":
		if m.regionCursor < len(awsRegions)-1 {
			m.regionCursor++
		}
	case "k", "up":
		if m.regionCursor > 0 {
			m.regionCursor--
		}
	case "enter":
		region := awsRegions[m.regionCursor]
		if region == m.cfg.Region {
			m.state = m.prevState
			return m, nil
		}
		m.cfg.Region = region
		m.state = stateLoading
		m.instances = nil
		m.filtered = nil
		m.selected = make(map[string]bool)
		m.cursor = 0
		m.offset = 0
		m.err = nil
		return m, m.fetchInstances
	}
	visible := m.regionVisibleRows()
	if m.regionCursor < m.regionOffset {
		m.regionOffset = m.regionCursor
	}
	if m.regionCursor >= m.regionOffset+visible {
		m.regionOffset = m.regionCursor - visible + 1
	}
	return m, nil
}

func (m Model) regionVisibleRows() int {
	rows := m.height - 4
	if rows < 1 {
		return 1
	}
	return rows
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

func (m Model) nameWidth() int {
	w := m.listWidth() - colFixed
	if w < minName {
		return minName
	}
	return w
}

func (m Model) showDetail() bool {
	return m.width > 130 && len(m.filtered) > 0
}

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	cursorStyle   = lipgloss.NewStyle().Background(lipgloss.Color("237")).Foreground(lipgloss.Color("15"))
	cursorBar     = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	normalStyle   = lipgloss.NewStyle()
	titleStyle    = lipgloss.NewStyle().Background(lipgloss.Color("208")).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	titleCtxStyle = lipgloss.NewStyle().Background(lipgloss.Color("208")).Foreground(lipgloss.Color("233")).Padding(0, 1)
	titleFill     = lipgloss.NewStyle().Background(lipgloss.Color("208"))
	filterLabel    = lipgloss.NewStyle().Background(lipgloss.Color("235")).Foreground(lipgloss.Color("208")).Bold(true).Padding(0, 1)
	statusStyle    = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Padding(0, 1)
	statusFill     = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	statusHintKey  = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Bold(true)
	statusHintDesc = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("245"))
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
	colID    = 21
	colState = 10
	colIP    = 16
	colSSM   = 8
	colFixed = colID + colState + colIP + colSSM + 4*2 + 2
	minName  = 15
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
	case stateRegionPicker:
		out.WriteString(m.viewRegionPicker())
	default:
		out.WriteString(m.viewReady())
	}
	return out.String()
}

func (m Model) viewRegionPicker() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Select region"))
	b.WriteString("  ")
	b.WriteString(detailLabel.Render("(current: " + m.cfg.Region + ")"))
	b.WriteString("\n\n")

	visible := m.regionVisibleRows()
	end := m.regionOffset + visible
	if end > len(awsRegions) {
		end = len(awsRegions)
	}

	for idx := m.regionOffset; idx < end; idx++ {
		region := awsRegions[idx]
		if idx == m.regionCursor {
			b.WriteString(cursorBar.Render("▌"))
			b.WriteString(cursorStyle.Render(" " + region))
		} else {
			b.WriteString("  " + region)
		}
		if region == m.cfg.Region {
			b.WriteString(detailLabel.Render(" (current)"))
		}
		b.WriteByte('\n')
	}

	b.WriteByte('\n')
	b.WriteString(detailLabel.Render("j/k to move, enter to select, esc to cancel"))
	return b.String()
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
		label := filterLabel.Render("FILTER")
		out.WriteString(label + " " + m.filter.View())
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

	out.WriteByte('\n')
	out.WriteString(m.renderStatus())
	return out.String()
}

func (m Model) renderStatus() string {
	showing := len(m.filtered)
	sel := len(m.selected)
	var left string
	if showing > 0 {
		visible := m.visibleRows()
		end := m.offset + visible
		if end > showing {
			end = showing
		}
		left = fmt.Sprintf("%d–%d of %d", m.offset+1, end, showing)
	} else {
		left = fmt.Sprintf("%d instances", len(m.instances))
	}
	if sel > 0 {
		left += fmt.Sprintf(" │ %d selected", sel)
	}
	if m.err != nil {
		left += fmt.Sprintf(" │ %v", m.err)
	}
	leftRendered := statusStyle.Render(left)

	hints := []struct{ key, desc string }{
		{"/", "filter"},
		{"space", "select"},
		{"enter", "connect"},
		{"r", "region"},
		{"q", "quit"},
	}
	var hb strings.Builder
	for i, h := range hints {
		if i > 0 {
			hb.WriteString(statusHintDesc.Render("  "))
		}
		hb.WriteString(statusHintKey.Render(h.key))
		hb.WriteString(statusHintDesc.Render(" "+h.desc))
	}
	rightRendered := statusStyle.Render(hb.String())

	gap := m.width - lipgloss.Width(leftRendered) - lipgloss.Width(rightRendered)
	if gap < 0 {
		fill := statusFill.Render(strings.Repeat(" ", max(0, m.width-lipgloss.Width(leftRendered))))
		return leftRendered + fill
	}
	fill := statusFill.Render(strings.Repeat(" ", gap))
	return leftRendered + fill + rightRendered
}

func (m Model) renderListRows() string {
	var b strings.Builder
	nw := m.nameWidth()

	header := formatRow(nw, "NAME", "INSTANCE ID", "STATE", "PRIVATE IP", "SSM", false)
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
		isCursor := idx == m.cursor
		isSelected := m.selected[inst.ID]
		var indicator string
		if isCursor {
			indicator = cursorBar.Render("▌")
		} else {
			indicator = " "
		}
		sel := " "
		if isSelected {
			sel = cursorBar.Render("*")
		}
		row := formatRow(
			nw,
			truncate(inst.Name, nw),
			inst.ID,
			inst.State,
			inst.PrivateIP,
			inst.SSMStatus,
			!isCursor,
		)
		if isCursor {
			b.WriteString(indicator + sel + cursorStyle.Render(row))
		} else {
			b.WriteString(indicator + sel + normalStyle.Render(row))
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
	dw := m.detailWidth()
	w := dw - 4

	var b strings.Builder

	title := inst.Name
	if title == "" {
		title = inst.ID
	}
	b.WriteString(detailValue.Bold(true).Render(truncate(title, w)))
	b.WriteByte('\n')
	b.WriteString(detailLabel.Render(strings.Repeat("─", w)))
	b.WriteByte('\n')

	const labelW = 12
	writeField := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(detailLabel.Render(fmt.Sprintf("%-*s", labelW, label)))
		b.WriteString(detailValue.Render(truncate(value, w-labelW)))
		b.WriteByte('\n')
	}

	writeStyledField := func(label, raw string, styled string) {
		if raw == "" {
			return
		}
		b.WriteString(detailLabel.Render(fmt.Sprintf("%-*s", labelW, label)))
		b.WriteString(styled)
		b.WriteByte('\n')
	}

	writeField("ID", inst.ID)
	writeStyledField("State", inst.State, colorState(inst.State))
	writeStyledField("SSM", inst.SSMStatus, colorSSM(inst.SSMStatus))

	b.WriteByte('\n')
	writeField("Private IP", inst.PrivateIP)
	writeField("Public IP", inst.PublicIP)
	writeField("AZ", inst.AZ)

	b.WriteByte('\n')
	writeField("AMI", inst.AMI)
	if !inst.LaunchTime.IsZero() {
		writeField("Launched", inst.LaunchTime.Format("2006-01-02 15:04"))
	}

	if len(inst.Tags) > 0 {
		b.WriteByte('\n')
		b.WriteString(detailLabel.Render("Tags"))
		b.WriteByte('\n')
		b.WriteString(detailLabel.Render(strings.Repeat("─", w/2)))
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
			b.WriteString(detailLabel.Render("  "+k+" "))
			b.WriteString(detailValue.Render(truncate(inst.Tags[k], w-len(k)-4)))
			b.WriteByte('\n')
		}
	}

	return detailBorder.Width(dw).Render(b.String())
}

func (m Model) detailWidth() int {
	w := m.width * 35 / 100
	if w < minDetailWidth {
		return minDetailWidth
	}
	if w > maxDetailWidth {
		return maxDetailWidth
	}
	return w
}

func (m Model) listWidth() int {
	if m.showDetail() {
		return m.width - m.detailWidth() - 4
	}
	return m.width
}

func formatRow(nameW int, name, id, state, ip, ssm string, colorize bool) string {
	if colorize {
		return pad(name, nameW) + "  " +
			pad(id, colID) + "  " +
			colorState(pad(state, colState)) + "  " +
			pad(ip, colIP) + "  " +
			colorSSM(pad(ssm, colSSM))
	}
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s",
		nameW, name,
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
