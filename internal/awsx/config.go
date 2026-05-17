package awsx

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

func LoadConfig(ctx context.Context, profile, region string) (aws.Config, error) {
	var opts []func(*config.LoadOptions) error
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}

func ListProfiles() []string {
	path := awsConfigPath()
	if path == "" {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var profiles []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[profile ") && strings.HasSuffix(line, "]") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "[profile "), "]")
			profiles = append(profiles, strings.TrimSpace(name))
		} else if line == "[default]" {
			profiles = append(profiles, "default")
		}
	}
	sort.Strings(profiles)
	return profiles
}

func awsConfigPath() string {
	if v := os.Getenv("AWS_CONFIG_FILE"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".aws", "config")
}
