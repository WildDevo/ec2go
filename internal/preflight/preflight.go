package preflight

import (
	"errors"
	"fmt"
	"strings"
)

func Check(lookup func(string) (string, error)) error {
	type tool struct {
		name string
		hint string
	}
	required := []tool{
		{"aws", "Install AWS CLI: https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"},
		{"session-manager-plugin", "Install Session Manager plugin: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html"},
		{"tmux", "Install tmux: https://github.com/tmux/tmux/wiki/Installing"},
	}

	var missing []tool
	for _, t := range required {
		if _, err := lookup(t.name); err != nil {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("missing required tools on PATH:\n")
	for _, t := range missing {
		fmt.Fprintf(&b, "  - %s\n    %s\n", t.name, t.hint)
	}
	return errors.New(b.String())
}
