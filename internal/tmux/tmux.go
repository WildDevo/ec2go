package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Pane struct {
	Title   string
	Command string
}

func Setup(panes []Pane) (string, error) {
	if len(panes) == 0 {
		return "", fmt.Errorf("no panes to create")
	}

	name := fmt.Sprintf("ec2go-%d", time.Now().Unix())

	firstPane, err := runOutput("tmux", "new-session", "-d", "-s", name, "-P", "-F", "#{pane_id}")
	if err != nil {
		return "", fmt.Errorf("new-session: %w", err)
	}
	paneIDs := []string{strings.TrimSpace(firstPane)}

	for i := 1; i < len(panes); i++ {
		paneID, err := runOutput("tmux", "split-window", "-t", name, "-P", "-F", "#{pane_id}")
		if err != nil {
			return "", fmt.Errorf("split-window pane %d: %w", i, err)
		}
		paneIDs = append(paneIDs, strings.TrimSpace(paneID))
	}

	if err := run("tmux", "select-layout", "-t", name, "tiled"); err != nil {
		return "", fmt.Errorf("select-layout: %w", err)
	}

	for i, p := range panes {
		if err := run("tmux", "send-keys", "-t", paneIDs[i], p.Command, "C-m"); err != nil {
			return "", fmt.Errorf("send-keys pane %d: %w", i, err)
		}
		if p.Title != "" {
			_ = run("tmux", "select-pane", "-t", paneIDs[i], "-T", p.Title)
		}
	}

	_ = run("tmux", "set-option", "-t", name, "pane-border-status", "top")

	return name, nil
}

func Attach(session string) error {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}

	args := []string{"tmux"}
	if insideTmux() {
		args = append(args, "switch-client", "-t", session)
	} else {
		args = append(args, "attach-session", "-t", session)
	}

	return execSyscall(tmuxPath, args, os.Environ())
}

func insideTmux() bool {
	return os.Getenv("TMUX") != ""
}

func run(name string, args ...string) error {
	_, err := runOutput(name, args...)
	return err
}

func runOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
