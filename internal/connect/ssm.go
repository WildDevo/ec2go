package connect

func BuildSSMArgs(instanceID, region, profile string) []string {
	args := []string{"ssm", "start-session", "--target", instanceID}
	if region != "" {
		args = append(args, "--region", region)
	}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	return args
}
