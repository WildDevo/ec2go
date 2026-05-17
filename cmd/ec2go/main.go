package main

import (
	"fmt"
	"os"
	"os/exec"

	"ec2go/internal/preflight"
)

func main() {
	if err := preflight.Check(exec.LookPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("ec2go")
}
