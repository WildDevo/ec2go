package session

import (
	"os/exec"

	"github.com/taigrr/bubbleterm/emulator"
)

// PaneConfig describes one terminal to run in the multiplexer.
type PaneConfig struct {
	Title string
	Cmd   string
	Args  []string
}

// Pane wraps a headless terminal emulator running a single child process.
type Pane struct {
	Title string

	emu    *emulator.Emulator
	exited chan struct{} // closed by the emulator when the child process exits
	done   bool          // set on the model goroutine once the exit is observed
}

// newPane starts cfg's command on a PTY sized to cols x rows.
func newPane(cfg PaneConfig, cols, rows int) (*Pane, error) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	emu, err := emulator.New(cols, rows)
	if err != nil {
		return nil, err
	}
	p := &Pane{Title: cfg.Title, emu: emu, exited: make(chan struct{})}
	emu.SetOnExit(func(string) { close(p.exited) })

	cmd := exec.Command(cfg.Cmd, cfg.Args...)
	if err := emu.StartCommand(cmd); err != nil {
		_ = emu.Close()
		return nil, err
	}
	// Enforce the intended size on the running child (delivers SIGWINCH).
	_ = emu.Resize(cols, rows)
	return p, nil
}

// rows returns the current screen, one ANSI-styled string per line.
func (p *Pane) rows() []string { return p.emu.GetScreen().Rows }

// cursor returns the child's cursor position within the pane content.
func (p *Pane) cursor() (emulator.Pos, bool) { return p.emu.Cursor() }

// sendKey forwards a pre-translated byte sequence to the child process.
func (p *Pane) sendKey(seq string) {
	if seq == "" {
		return
	}
	_ = p.emu.SendKey(seq)
}

func (p *Pane) resize(cols, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	_ = p.emu.Resize(cols, rows)
}

func (p *Pane) close() { _ = p.emu.Close() }

// changed signals each time the screen content changes.
func (p *Pane) changed() <-chan struct{} { return p.emu.NotifyChanged() }
