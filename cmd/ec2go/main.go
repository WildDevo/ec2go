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

	instances, err := awsx.ListInstances(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list instances: %v\n", err)
		os.Exit(1)
	}

	for _, i := range instances {
		fmt.Printf("%s\t%s\t%s\t%s\n", i.Name, i.ID, i.State, i.PrivateIP)
	}
	if len(instances) == 0 {
		fmt.Println("no instances found")
	}
}
