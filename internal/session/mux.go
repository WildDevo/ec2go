package session

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// SessionFinishedMsg is emitted when every pane has exited or the user quit
// the multiplexer. The host program handles it (return to its main view, or
// tea.Quit for a standalone run).
type SessionFinishedMsg struct{}

type paneDirtyMsg struct{ idx int }
type paneExitedMsg struct{ idx int }

var (
	paneTitle        = lipgloss.NewStyle().Background(lipgloss.Color("238")).Foreground(lipgloss.Color("252")).Bold(true)
	paneTitleFocused = lipgloss.NewStyle().Background(lipgloss.Color("208")).Foreground(lipgloss.Color("0")).Bold(true)
	sepStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	statusBar        = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("250"))
	statusSyncOn     = lipgloss.NewStyle().Background(lipgloss.Color("22")).Foreground(lipgloss.Color("15")).Bold(true)
	statusSyncOff    = lipgloss.NewStyle().Background(lipgloss.Color("94")).Foreground(lipgloss.Color("15")).Bold(true)
)

// Mux is a bubbletea model that tiles several terminal panes and broadcasts
// input to them. It is used embedded in a host model (delegated to while in a
// session state) and can also be driven standalone.
type Mux struct {
	panes    []*Pane
	focused  int
	sync     bool
	prefix   bool // previous key was the Ctrl+A prefix
	width    int
	height   int
	Finished bool
}

// New starts a pane per config, sized to fit a width x height screen.
func New(configs []PaneConfig, width, height int) (*Mux, error) {
	if width < 10 || height < 5 {
		width, height = 80, 24
	}
	m := &Mux{sync: true, width: width, height: height}
	boxes := layout(len(configs), width, height-1)
	for i, cfg := range configs {
		p, err := newPane(cfg, boxes[i].w, boxes[i].h)
		if err != nil {
			for _, pp := range m.panes {
				pp.close()
			}
			return nil, fmt.Errorf("start pane %q: %w", cfg.Title, err)
		}
		m.panes = append(m.panes, p)
	}
	return m, nil
}

func (m *Mux) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.panes)*2)
	for i, p := range m.panes {
		cmds = append(cmds, watchChanged(i, p), watchExited(i, p))
	}
	return tea.Batch(cmds...)
}

func (m *Mux) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizePanes()
		return m, nil
	case paneDirtyMsg:
		if msg.idx < len(m.panes) {
			return m, watchChanged(msg.idx, m.panes[msg.idx])
		}
		return m, nil
	case paneExitedMsg:
		if msg.idx < len(m.panes) {
			m.panes[msg.idx].done = true
		}
		if m.allDone() {
			return m.finish()
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Mux) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()

	if m.prefix {
		m.prefix = false
		switch s {
		case "s":
			m.sync = !m.sync
			return m, nil
		case "q":
			return m.finish()
		case "ctrl+a":
			m.broadcast("\x01") // literal Ctrl+A
			return m, nil
		case "tab", "right", "down":
			m.focusNext()
			return m, nil
		case "left", "up":
			m.focusPrev()
			return m, nil
		}
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			if idx := int(s[0] - '1'); idx < len(m.panes) {
				m.focused = idx
			}
		}
		return m, nil
	}

	if s == "ctrl+a" {
		m.prefix = true
		return m, nil
	}

	m.broadcast(keyToBytes(msg))
	return m, nil
}

// broadcast sends seq to all panes when synchronized, else the focused pane.
func (m *Mux) broadcast(seq string) {
	if seq == "" {
		return
	}
	if m.sync {
		for _, p := range m.panes {
			if !p.done {
				p.sendKey(seq)
			}
		}
		return
	}
	if m.focused < len(m.panes) && !m.panes[m.focused].done {
		m.panes[m.focused].sendKey(seq)
	}
}

func (m *Mux) focusNext() {
	if len(m.panes) > 0 {
		m.focused = (m.focused + 1) % len(m.panes)
	}
}

func (m *Mux) focusPrev() {
	if len(m.panes) > 0 {
		m.focused = (m.focused - 1 + len(m.panes)) % len(m.panes)
	}
}

func (m *Mux) allDone() bool {
	for _, p := range m.panes {
		if !p.done {
			return false
		}
	}
	return true
}

func (m *Mux) finish() (tea.Model, tea.Cmd) {
	m.Finished = true
	for _, p := range m.panes {
		p.close()
	}
	return m, func() tea.Msg { return SessionFinishedMsg{} }
}

func (m *Mux) resizePanes() {
	boxes := m.computeBoxes()
	for i, p := range m.panes {
		if i < len(boxes) {
			p.resize(boxes[i].w, boxes[i].h)
		}
	}
}

func (m *Mux) computeBoxes() []paneBox {
	h := m.height - 1 // reserve the status line
	if h < 1 {
		h = 1
	}
	w := m.width
	if w < 1 {
		w = 1
	}
	return layout(len(m.panes), w, h)
}

func (m *Mux) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true

	if m.focused < len(m.panes) {
		p := m.panes[m.focused]
		if !p.done {
			boxes := m.computeBoxes()
			if m.focused < len(boxes) {
				b := boxes[m.focused]
				if cp, ok := p.cursor(); ok {
					v.Cursor = tea.NewCursor(b.x+clamp(cp.X, 0, b.w-1), b.y+clamp(cp.Y, 0, b.h-1))
					v.Cursor.Blink = true
				}
			}
		}
	}
	return v
}

func (m *Mux) render() string {
	boxes := m.computeBoxes()
	cols, rows := grid(len(m.panes))

	rowStrs := make([]string, 0, rows)
	idx := 0
	for r := 0; r < rows && idx < len(m.panes); r++ {
		panesInRow := cols
		if rem := len(m.panes) - r*cols; rem < cols {
			panesInRow = rem
		}
		rowH := boxes[idx].h + 1 // title + content
		parts := make([]string, 0, panesInRow*2)
		for c := 0; c < panesInRow; c++ {
			if c > 0 {
				parts = append(parts, m.sepBlock(rowH))
			}
			parts = append(parts, m.renderPane(m.panes[idx], boxes[idx], idx == m.focused))
			idx++
		}
		rowStrs = append(rowStrs, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rowStrs...)
	return content + "\n" + m.renderStatus()
}

func (m *Mux) sepBlock(height int) string {
	line := sepStyle.Render("│")
	lines := make([]string, height)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func (m *Mux) renderPane(p *Pane, b paneBox, focused bool) string {
	title := p.Title
	if p.done {
		title += " · ended"
	}
	ts := paneTitle
	if focused {
		ts = paneTitleFocused
	}

	var sb strings.Builder
	sb.WriteString(ts.Width(b.w).Render(ansi.Truncate(title, b.w, "")))
	sb.WriteByte('\n')

	prows := p.rows()
	for y := 0; y < b.h; y++ {
		var line string
		if y < len(prows) {
			line = prows[y]
		}
		sb.WriteString(fitLine(line, b.w))
		if y < b.h-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func (m *Mux) renderStatus() string {
	var mode string
	if m.sync {
		mode = statusSyncOn.Render(" SYNC ON ")
	} else {
		mode = statusSyncOff.Render(fmt.Sprintf(" SYNC OFF · pane %d/%d ", m.focused+1, len(m.panes)))
	}
	hints := statusBar.Render("  ^A s sync · ^A ⇥ focus · ^A q quit ")

	out := mode + hints
	if w := ansi.StringWidth(out); w < m.width {
		out += statusBar.Render(strings.Repeat(" ", m.width-w))
	}
	return out
}

// fitLine pads or truncates an ANSI-styled line to exactly w display cells.
func fitLine(s string, w int) string {
	sw := ansi.StringWidth(s)
	switch {
	case sw > w:
		return ansi.Truncate(s, w, "")
	case sw < w:
		return s + strings.Repeat(" ", w-sw)
	default:
		return s
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func watchChanged(idx int, p *Pane) tea.Cmd {
	return func() tea.Msg {
		<-p.changed()
		return paneDirtyMsg{idx: idx}
	}
}

func watchExited(idx int, p *Pane) tea.Cmd {
	return func() tea.Msg {
		<-p.exited
		return paneExitedMsg{idx: idx}
	}
}
