package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"ec2go/internal/awsx"
	"ec2go/internal/preflight"
	"ec2go/internal/tui"
)

func main() {
	profileFlag := flag.String("profile", "", "AWS profile to use")
	regionFlag := flag.String("region", "", "AWS region to use")
	flag.Parse()

	if err := preflight.Check(exec.LookPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	profile := resolveProfile(*profileFlag)
	region := *regionFlag
	if region == "" {
		region = os.Getenv("AWS_REGION")
		if region == "" {
			region = os.Getenv("AWS_DEFAULT_REGION")
		}
	}

	ctx := context.Background()
	cfg, err := awsx.LoadConfig(ctx, profile, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load AWS config: %v\n", err)
		os.Exit(1)
	}

	if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
		if awsx.IsSSO(profile) {
			fmt.Fprintln(os.Stderr, "SSO session expired or not started, logging in...")
			loginArgs := []string{"sso", "login"}
			if profile != "" {
				loginArgs = append(loginArgs, "--profile", profile)
			}
			loginCmd := exec.Command("aws", loginArgs...)
			loginCmd.Stdin = os.Stdin
			loginCmd.Stdout = os.Stdout
			loginCmd.Stderr = os.Stderr
			if err := loginCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "sso login failed: %v\n", err)
				os.Exit(1)
			}
			cfg, err = awsx.LoadConfig(ctx, profile, region)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to reload AWS config after login: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Fprintf(os.Stderr, "failed to retrieve AWS credentials: %v\n", err)
			os.Exit(1)
		}
	}

	if cfg.Region == "" {
		fmt.Fprintln(os.Stderr, "no AWS region configured")
		fmt.Fprintln(os.Stderr, "set one with: --region, AWS_REGION, or in ~/.aws/config for your profile")
		os.Exit(1)
	}

	p := tea.NewProgram(tui.New(cfg, profile))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func resolveProfile(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("AWS_PROFILE"); v != "" {
		return v
	}

	profiles := awsx.ListProfiles()
	if len(profiles) == 0 {
		return ""
	}
	if len(profiles) == 1 {
		return profiles[0]
	}

	selected, err := pickProfile(profiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile selection: %v\n", err)
		os.Exit(1)
	}
	return selected
}
