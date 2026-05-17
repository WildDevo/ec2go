package awsx

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type Instance struct {
	ID        string
	Name      string
	State     string
	AZ        string
	PrivateIP string
	PublicIP  string
	Tags      map[string]string
}

func ListInstances(ctx context.Context, cfg aws.Config) ([]Instance, error) {
	client := ec2.NewFromConfig(cfg)
	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"pending", "running", "shutting-down", "stopping"},
			},
		},
	})

	var instances []Instance
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Reservations {
			for _, i := range r.Instances {
				instances = append(instances, toInstance(i))
			}
		}
	}
	return instances, nil
}

func toInstance(i types.Instance) Instance {
	inst := Instance{
		ID:    deref(i.InstanceId),
		State: string(i.State.Name),
		AZ:    deref(i.Placement.AvailabilityZone),
		Tags:  make(map[string]string, len(i.Tags)),
	}
	if i.PrivateIpAddress != nil {
		inst.PrivateIP = *i.PrivateIpAddress
	}
	if i.PublicIpAddress != nil {
		inst.PublicIP = *i.PublicIpAddress
	}
	for _, t := range i.Tags {
		key := deref(t.Key)
		inst.Tags[key] = deref(t.Value)
		if key == "Name" {
			inst.Name = deref(t.Value)
		}
	}
	return inst
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
