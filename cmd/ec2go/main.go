package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"ec2go/internal/awsx"
	"ec2go/internal/preflight"
)

func main() {
	if err := preflight.Check(exec.LookPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	cfg, err := awsx.LoadConfig(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load AWS config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ec2go: region=%s\n", cfg.Region)
}
