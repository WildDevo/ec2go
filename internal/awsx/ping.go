package awsx

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

func PingStatus(ctx context.Context, cfg aws.Config) (map[string]string, error) {
	client := ssm.NewFromConfig(cfg)
	paginator := ssm.NewDescribeInstanceInformationPaginator(client, &ssm.DescribeInstanceInformationInput{})

	status := make(map[string]string)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, info := range page.InstanceInformationList {
			if info.InstanceId != nil {
				status[*info.InstanceId] = string(info.PingStatus)
			}
		}
	}
	return status, nil
}

func MergeSSMStatus(instances []Instance, status map[string]string) []Instance {
	var result []Instance
	for _, inst := range instances {
		inst.SSMStatus = status[inst.ID]
		if inst.SSMStatus == "Online" {
			result = append(result, inst)
		}
	}
	return result
}
