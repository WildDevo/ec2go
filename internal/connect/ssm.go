package connect

func BuildSSMArgs(instanceID, region string) []string {
	args := []string{"ssm", "start-session", "--target", instanceID}
	if region != "" {
		args = append(args, "--region", region)
	}
	return args
}
