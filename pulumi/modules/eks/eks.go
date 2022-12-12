package eks

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/autoscaling"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ec2"
	awseks "github.com/pulumi/pulumi-aws/sdk/v5/go/aws/eks"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ssm"
	eks "github.com/pulumi/pulumi-eks/sdk/go/eks"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"github.com/voltrondata/pulumi-go-modules/shared/utilities"
)

func errorHandler(err error) {
	if err != nil {
		panic(err)
	}
}

type EksConfig struct {
	Name              string
	Version           string
	Tags              map[string]string
	LinuxNodegroups   map[string]map[string]string
	WindowsNodegroups map[string]map[string]string
}

type EksOutput struct {
	EksClusterOutput    awseks.ClusterOutput
	LinuxNodeGroupRoles map[string]*iam.Role
	LinuxNodeGroups     []*awseks.NodeGroup
	WindowsNodeGroups   []*autoscaling.Group
}

type TemplateInput struct {
	ClusterName        string
	BootstrapArguments string
	AwsRegion          string
}

func CreateEKSCluster(ctx *pulumi.Context, vpc *ec2.Vpc, subnets []*ec2.Subnet) (EksOutput, error) {

	// Get the EKS config from context
	EksConfig := &EksConfig{}
	conf := config.New(ctx, "")
	conf.RequireObject("Eks", &EksConfig)

	// Create a pulumiStringMap for the Tags
	CommonTags := pulumi.StringMap{}
	for index, tag := range EksConfig.Tags {
		CommonTags[index] = pulumi.String(tag)
	}
	// Initialize the output struct
	EksOutput := &EksOutput{}

	// Assume Role for the EKS cluster
	eksRole, err := iam.NewRole(ctx, "eks-iam-eksRole", &iam.RoleArgs{
		Name:        pulumi.String(EksConfig.Name + "-eks-role"),
		Description: pulumi.String("Role for" + EksConfig.Name + "EKS cluster"),
		AssumeRolePolicy: pulumi.String(`{
		"Version": "2008-10-17",
		"Statement": [{
			"Sid": "",
			"Effect": "Allow",
			"Principal": {
				"Service": "eks.amazonaws.com"
			},
			"Action": "sts:AssumeRole"
		}]
	}`),
		Tags: pulumi.StringMap(CommonTags),
	})
	errorHandler(err)

	// attachment of policies to the EKS Role
	eksPolicies := []string{
		"arn:aws:iam::aws:policy/AmazonEKSServicePolicy",
		"arn:aws:iam::aws:policy/AmazonEKSClusterPolicy",
		"arn:aws:iam::aws:policy/AmazonEKSVPCResourceController",
	}
	for i, eksPolicy := range eksPolicies {
		_, err := iam.NewRolePolicyAttachment(ctx, fmt.Sprintf("rpa-%d", i), &iam.RolePolicyAttachmentArgs{
			PolicyArn: pulumi.String(eksPolicy),
			Role:      eksRole.Name,
		})
		errorHandler(err)
	}

	// Create the roles for all linux nodegroups, so we can add them to the aws-auth automatically.
	// Not possible to use the same approach for Windows node groups since we have to also add the role to eks:kube-proxy-windows group
	// So that step will still be done by hand as described on the README.md
	linuxNodeGroupRoleArray := iam.RoleArray{}
	EksOutput.LinuxNodeGroupRoles, linuxNodeGroupRoleArray = createLinuxNodeGroupRoles(ctx, EksConfig, CommonTags)

	// Create EKS Cluster
	eksCluster, err := eks.NewCluster(ctx, "eks-cluster", &eks.ClusterArgs{
		CreateOidcProvider: pulumi.Bool(true),
		Name:               pulumi.String(EksConfig.Name),
		PublicAccessCidrs: pulumi.StringArray{
			pulumi.String("0.0.0.0/0"),
		},
		ServiceRole:          eksRole,
		SkipDefaultNodeGroup: pulumi.Bool(true),
		SubnetIds:            getSubnetIds(subnets),
		Tags:                 pulumi.StringMap(CommonTags),
		Version:              pulumi.String(EksConfig.Version),
		VpcId:                vpc.ID(),
		InstanceRoles:        linuxNodeGroupRoleArray,
	})
	errorHandler(err)

	EksOutput.EksClusterOutput = eksCluster.EksCluster

	////////////////////////////////////////
	// Linux Node Groups////////////////////
	////////////////////////////////////////
	EksOutput.LinuxNodeGroups = createLinuxNodeGroups(ctx, EksConfig, CommonTags, subnets, eksCluster, EksOutput.LinuxNodeGroupRoles)

	/////////////////////////////////////////
	// Windows Node Groups///////////////////
	/////////////////////////////////////////
	EksOutput.WindowsNodeGroups = createWindowsNodeGroups(ctx, EksConfig, CommonTags, vpc, subnets, eksCluster, EksOutput)

	err = createAutoScalerIamResources(ctx, eksCluster)
	errorHandler(err)

	return *EksOutput, nil
}

func getSubnetIds(subnets []*ec2.Subnet) pulumi.StringArray {
	var subnetIds []pulumi.IDOutput
	for _, subnet := range subnets {
		subnetIds = append(subnetIds, subnet.ID())
	}
	return utilities.IdOutputArrayToStringOutputArray(subnetIds)
}

func generatePowershellTemplate(clusterName string, region string) string {
	tplstring := `<powershell>
[string]$EKSBinDir = "$env:ProgramFiles\Amazon\EKS"
[string]$EKSBootstrapScriptName = 'Start-EKSBootstrap.ps1'
[string]$EKSBootstrapScriptFile = "$EKSBinDir\$EKSBootstrapScriptName"
[string]$cfn_signal = "$env:ProgramFiles\Amazon\cfn-bootstrap\cfn-signal.exe"
& $EKSBootstrapScriptFile -EKSClusterName {{.ClusterName}} {{.BootstrapArguments}} 3>&1 4>&1 5>&1 6>&1
$LastError = if ($?) { 0 } else { $Error[0].Exception.HResult }
& $cfn_signal --exit-code=$LastError ` + "`" + `
  --resource="NodeGroup" ` + "`" + `
  --region={{.AwsRegion}}
</powershell>`

	tpl, err := template.New("Template").Parse(tplstring)
	errorHandler(err)

	tplInput := TemplateInput{
		ClusterName:        clusterName,
		BootstrapArguments: "-ContainerRuntime containerd",
		AwsRegion:          region,
	}
	var tplBytes bytes.Buffer
	err = tpl.Execute(&tplBytes, tplInput)
	errorHandler(err)

	return base64.StdEncoding.EncodeToString([]byte(tplBytes.Bytes()))
}

func createAutoScalerIamResources(ctx *pulumi.Context, eksCluster *eks.Cluster) error {
	autoScalingPolicyJson, err := json.Marshal(map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			map[string]interface{}{
				"Action": []string{
					"autoscaling:DescribeAutoScalingGroups",
					"autoscaling:DescribeAutoScalingInstances",
					"autoscaling:DescribeLaunchConfigurations",
					"autoscaling:DescribeTags",
					"autoscaling:SetDesiredCapacity",
					"autoscaling:TerminateInstanceInAutoScalingGroup",
					"ec2:DescribeLaunchTemplateVersions",
					"ec2:DescribeInstanceTypes",
				},
				"Effect":   "Allow",
				"Resource": "*",
			},
		},
	})
	errorHandler(err)

	// Create the IAM policy for the AutoScaler
	autoScalingPolicy, err := iam.NewPolicy(ctx, "AmazonEKSClusterAutoscalerPolicy", &iam.PolicyArgs{
		Name:        pulumi.String("AmazonEKSClusterAutoscalerPolicy"),
		Description: pulumi.String("Policy for the Kubernetes AutoScaler"),
		Path:        pulumi.String("/"),
		Policy:      pulumi.String(autoScalingPolicyJson),
	})
	errorHandler(err)

	_ = eksCluster.EksCluster.Identities().ApplyT(func(identities []awseks.ClusterIdentity) error {
		oidcUrl := *identities[0].Oidcs[0].Issuer
		oidcName := strings.ReplaceAll(oidcUrl, "https://", "")

		currentCaller, err := aws.GetCallerIdentity(ctx, nil, nil)
		errorHandler(err)

		oidcArn := fmt.Sprintf("arn:aws:iam::%s:oidc-provider/%s", currentCaller.AccountId, oidcName)

		oidcProvider, err := iam.GetOpenIdConnectProvider(ctx, oidcName, pulumi.ID(oidcArn), nil)
		errorHandler(err)

		oidcProviderClientID := oidcProvider.ClientIdLists.ApplyT(func(clientIdLists []string) string {
			return clientIdLists[0]
		}).(pulumi.StringOutput)

		oidcProviderConfigName := pulumi.String("oidcProviderConfig")
		oidcProviderIssuerUrl := pulumi.String(oidcUrl)

		clusterName := eksCluster.EksCluster.Name().ApplyT(func(clusterName string) string {
			return clusterName
		}).(pulumi.StringOutput)

		_, err = awseks.NewIdentityProviderConfig(ctx, "example", &awseks.IdentityProviderConfigArgs{
			ClusterName: clusterName,
			Oidc: &awseks.IdentityProviderConfigOidcArgs{
				ClientId:                   oidcProviderClientID,
				IdentityProviderConfigName: oidcProviderConfigName,
				IssuerUrl:                  oidcProviderIssuerUrl,
			},
		})

		errorHandler(err)
		return nil
	})

	assumeRolePolicyJson := eksCluster.EksCluster.Identities().ApplyT(func(identities []awseks.ClusterIdentity) string {
		oidcUrl := *identities[0].Oidcs[0].Issuer
		oidcName := strings.ReplaceAll(oidcUrl, "https://", "")

		currentCaller, err := aws.GetCallerIdentity(ctx, nil, nil)
		errorHandler(err)

		oidcArn := fmt.Sprintf("arn:aws:iam::%s:oidc-provider/%s", currentCaller.AccountId, oidcName)

		assumeRolePolicyJson, err := json.Marshal(map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				map[string]interface{}{
					"Action": "sts:AssumeRoleWithWebIdentity",
					"Effect": "Allow",
					"Principal": map[string]interface{}{
						"Federated": oidcArn,
					},
					"Condition": map[string]interface{}{
						"StringEquals": map[string]interface{}{
							fmt.Sprintf("%s:sub", oidcName): "system:serviceaccount:kube-system:cluster-autoscaler",
						},
					},
				},
			},
		})
		errorHandler(err)

		return string(assumeRolePolicyJson)
	}).(pulumi.StringOutput)

	autoScalerRole, err := iam.NewRole(ctx, "AmazonEKSClusterAutoscalerRole", &iam.RoleArgs{
		Name:             pulumi.String("AmazonEKSClusterAutoscalerRole"),
		AssumeRolePolicy: assumeRolePolicyJson,
		ManagedPolicyArns: pulumi.StringArray{
			autoScalingPolicy.Arn,
		},
	})
	errorHandler(err)

	ctx.Export("autoScalerRoleArn", autoScalerRole.Arn)
	return nil
}

func createLinuxNodeGroupRoles(ctx *pulumi.Context, EksConfig *EksConfig, CommonTags pulumi.StringMap) (map[string]*iam.Role, iam.RoleArray) {
	linuxNodeGroupRoles := map[string]*iam.Role{}
	arrayLinuxNodeGroupRoles := iam.RoleArray{}
	for key := range EksConfig.LinuxNodegroups {

		// Assume Role for the node group
		nodeGroupRole, err := iam.NewRole(ctx, EksConfig.LinuxNodegroups[key]["name"]+"-role", &iam.RoleArgs{
			Name:        pulumi.String(EksConfig.LinuxNodegroups[key]["name"] + "-role"),
			Description: pulumi.String("Role used by" + EksConfig.LinuxNodegroups[key]["name"] + " nodegroup of" + EksConfig.Name + "EKS cluster"),
			AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Sid": "",
				"Effect": "Allow",
				"Principal": {
					"Service": "ec2.amazonaws.com"
				},
				"Action": "sts:AssumeRole"
			}]
		}`),
			Tags: pulumi.StringMap(CommonTags),
		})
		errorHandler(err)

		// Exporting the role ARN for the aws-auth configMap. Only need to modify aws-auth if there are any Windows node groups.
		ctx.Export(EksConfig.LinuxNodegroups[key]["name"]+"-role-arn", nodeGroupRole.Arn)

		//policies attachment to the nodegroup Role
		nodeGroupPolicies := []string{
			"arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
			"arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
			"arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
			"arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore",
		}
		for i, nodeGroupPolicy := range nodeGroupPolicies {
			_, err := iam.NewRolePolicyAttachment(ctx, fmt.Sprintf(EksConfig.LinuxNodegroups[key]["name"]+"-role-pa-%d", i), &iam.RolePolicyAttachmentArgs{
				Role:      nodeGroupRole.Name,
				PolicyArn: pulumi.String(nodeGroupPolicy),
			})
			errorHandler(err)
		}
		errorHandler(err)
		linuxNodeGroupRoles[key] = nodeGroupRole
		arrayLinuxNodeGroupRoles = append(arrayLinuxNodeGroupRoles, nodeGroupRole)
	}
	return linuxNodeGroupRoles, arrayLinuxNodeGroupRoles
}

func createLinuxNodeGroups(ctx *pulumi.Context, EksConfig *EksConfig, CommonTags pulumi.StringMap, subnets []*ec2.Subnet, eksCluster *eks.Cluster, linuxNodeGroupRoles map[string]*iam.Role) []*awseks.NodeGroup {

	nodeGroups := []*awseks.NodeGroup{}
	for key := range EksConfig.LinuxNodegroups {
		desiredSize, err := strconv.Atoi(EksConfig.LinuxNodegroups[key]["desiredSize"])
		errorHandler(err)
		maxSize, err := strconv.Atoi(EksConfig.LinuxNodegroups[key]["maxSize"])
		errorHandler(err)
		minSize, err := strconv.Atoi(EksConfig.LinuxNodegroups[key]["minSize"])
		errorHandler(err)
		diskSize, err := strconv.Atoi(EksConfig.LinuxNodegroups[key]["diskSize"])
		errorHandler(err)

		// Adding cluster autoscaler tags
		clusterName := eksCluster.EksCluster.Name().ApplyT(func(clusterName string) string {
			CommonTags["k8s.io/cluster-autoscaler/"+clusterName] = pulumi.String("owned")
			return clusterName
		}).(pulumi.StringOutput)

		CommonTags["k8s.io/cluster-autoscaler/enabled"] = pulumi.String("true")

		// Creating the node group
		nodeGroup, err := awseks.NewNodeGroup(ctx, EksConfig.LinuxNodegroups[key]["name"], &awseks.NodeGroupArgs{
			ClusterName:   clusterName,
			NodeGroupName: pulumi.String(EksConfig.LinuxNodegroups[key]["name"]),
			NodeRoleArn:   pulumi.StringInput(linuxNodeGroupRoles[key].Arn),
			SubnetIds:     getSubnetIds(subnets),
			InstanceTypes: pulumi.StringArray{pulumi.String(EksConfig.LinuxNodegroups[key]["instanceType"])},
			AmiType:       pulumi.String(EksConfig.LinuxNodegroups[key]["amiType"]),
			DiskSize:      pulumi.Int(diskSize),
			ScalingConfig: &awseks.NodeGroupScalingConfigArgs{
				DesiredSize: pulumi.Int(desiredSize),
				MaxSize:     pulumi.Int(maxSize),
				MinSize:     pulumi.Int(minSize),
			},
			RemoteAccess: &awseks.NodeGroupRemoteAccessArgs{
				Ec2SshKey: pulumi.String(EksConfig.LinuxNodegroups[key]["sshKey"]),
			},
			Tags: pulumi.StringMap(CommonTags),
		})
		errorHandler(err)
		nodeGroups = append(nodeGroups, nodeGroup)

	}
	return nodeGroups
}

func createWindowsNodeGroups(ctx *pulumi.Context, EksConfig *EksConfig, CommonTags pulumi.StringMap, vpc *ec2.Vpc, subnets []*ec2.Subnet, eksCluster *eks.Cluster, EksOutput *EksOutput) []*autoscaling.Group {

	conf := config.New(ctx, "")
	windowsNodeGroups := []*autoscaling.Group{}
	// AMI lookup for the optimized version of the cluster
	windowsAMI, err := ssm.LookupParameter(ctx, &ssm.LookupParameterArgs{
		Name: "/aws/service/ami-windows-latest/Windows_Server-2019-English-Core-EKS_Optimized-" + EksConfig.Version + "/image_id",
	}, nil)
	errorHandler(err)

	// Cluster Name Tag required for Windows autoscaling groups
	clusterNameTag := eksCluster.EksCluster.Name().ApplyT(func(clusterName string) string {
		return "kubernetes.io/cluster/" + clusterName
	}).(pulumi.StringOutput)

	sgID := eksCluster.EksCluster.VpcConfig().ClusterSecurityGroupId().Elem()

	for key := range EksConfig.WindowsNodegroups {

		desiredSize, err := strconv.Atoi(EksConfig.WindowsNodegroups[key]["desiredSize"])
		errorHandler(err)
		maxSize, err := strconv.Atoi(EksConfig.WindowsNodegroups[key]["maxSize"])
		errorHandler(err)
		minSize, err := strconv.Atoi(EksConfig.WindowsNodegroups[key]["minSize"])
		errorHandler(err)
		diskSize, err := strconv.Atoi(EksConfig.WindowsNodegroups[key]["diskSize"])
		errorHandler(err)

		// Security Group that that allows connection to the cluster
		windowsNodegroupSg, err := ec2.NewSecurityGroup(ctx, EksConfig.WindowsNodegroups[key]["name"]+"-sg", &ec2.SecurityGroupArgs{
			Name:        pulumi.String(EksConfig.WindowsNodegroups[key]["name"] + "-sg"),
			Description: pulumi.String("Windows nodegroup, Allow inbound from itself and eks cluster on port 10250"),
			VpcId:       vpc.ID(),
			Egress: ec2.SecurityGroupEgressArray{
				ec2.SecurityGroupEgressArgs{
					Protocol:   pulumi.String("-1"),
					FromPort:   pulumi.Int(0),
					ToPort:     pulumi.Int(0),
					CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				},
			},
			Ingress: ec2.SecurityGroupIngressArray{
				ec2.SecurityGroupIngressArgs{
					Protocol: pulumi.String("-1"),
					FromPort: pulumi.Int(0),
					ToPort:   pulumi.Int(0),
					Self:     pulumi.Bool(true),
				},
				ec2.SecurityGroupIngressArgs{
					Protocol:       pulumi.String("tcp"),
					FromPort:       pulumi.Int(10250),
					ToPort:         pulumi.Int(10250),
					SecurityGroups: pulumi.StringArray{sgID},
				},
			},
			Tags: pulumi.StringMap(CommonTags),
		})
		errorHandler(err)

		// Need to define an inbound on the default cluster security group allowing traffic
		// from windows nodegroup security group
		_, err = ec2.NewSecurityGroupRule(ctx, EksConfig.WindowsNodegroups[key]["name"]+"-sg-inbound-in-cluster-sg", &ec2.SecurityGroupRuleArgs{
			Type:                  pulumi.String("ingress"),
			FromPort:              pulumi.Int(0),
			ToPort:                pulumi.Int(0),
			Protocol:              pulumi.String("-1"),
			SecurityGroupId:       sgID,
			SourceSecurityGroupId: windowsNodegroupSg.ID(),
		}, pulumi.DependsOn([]pulumi.Resource{eksCluster}))
		errorHandler(err)
		// Assume Role for the node group
		windowsNodeGroupRole, err := iam.NewRole(ctx, EksConfig.WindowsNodegroups[key]["name"]+"-role", &iam.RoleArgs{
			Name:        pulumi.String(EksConfig.WindowsNodegroups[key]["name"] + "-role"),
			Description: pulumi.String("Role used by" + EksConfig.WindowsNodegroups[key]["name"] + " nodegroup of" + EksConfig.Name + "EKS cluster"),
			AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Sid": "",
				"Effect": "Allow",
				"Principal": {
					"Service": "ec2.amazonaws.com"
				},
				"Action": "sts:AssumeRole"
			}]
		}`),
			ManagedPolicyArns: pulumi.StringArray{
				pulumi.String("arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy"),
				pulumi.String("arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy"),
				pulumi.String("arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"),
				pulumi.String("arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"),
			},
			Tags: pulumi.StringMap(CommonTags),
		})
		errorHandler(err)

		// Exporting the role ARN for the aws-auth configMap
		ctx.Export(EksConfig.WindowsNodegroups[key]["name"]+"-role-arn", windowsNodeGroupRole.Arn)

		// attachment of policies to the nodegroup Role
		windowsNodeGroupPolicies := []string{
			"arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
			"arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
			"arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
			"arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore",
		}

		for i, nodeGroupPolicy := range windowsNodeGroupPolicies {
			_, err := iam.NewRolePolicyAttachment(ctx, fmt.Sprintf(EksConfig.WindowsNodegroups[key]["name"]+"-role-pa-%d", i), &iam.RolePolicyAttachmentArgs{
				Role:      windowsNodeGroupRole.Name,
				PolicyArn: pulumi.String(nodeGroupPolicy),
			})
			errorHandler(err)
		}
		windowsInstanceProfile, err := iam.NewInstanceProfile(ctx, EksConfig.WindowsNodegroups[key]["name"]+"-instance-profile", &iam.InstanceProfileArgs{
			Name: pulumi.String(EksConfig.WindowsNodegroups[key]["name"] + "-instance-profile"),
			Role: windowsNodeGroupRole.Name,
		})
		errorHandler(err)

		clusterName := eksCluster.EksCluster.Name().ApplyT(func(name string) string {
			return name
		}).(pulumi.StringOutput)

		templateb64encoded := pulumi.All(clusterName, conf.Require("region")).ApplyT(
			func(args []interface{}) (string, error) {
				clusterName := args[0].(string)
				region := args[1].(string)
				return generatePowershellTemplate(clusterName, region), err
			},
		).(pulumi.StringOutput)
		errorHandler(err)

		windowsLaunchTemplate, err := ec2.NewLaunchTemplate(ctx, EksConfig.WindowsNodegroups[key]["name"]+"-launch-template", &ec2.LaunchTemplateArgs{
			Name: pulumi.String(EksConfig.WindowsNodegroups[key]["name"] + "-launch-template"),
			BlockDeviceMappings: ec2.LaunchTemplateBlockDeviceMappingArray{
				&ec2.LaunchTemplateBlockDeviceMappingArgs{
					DeviceName: pulumi.String("/dev/sda1"),
					Ebs: &ec2.LaunchTemplateBlockDeviceMappingEbsArgs{
						VolumeSize:          pulumi.Int(diskSize),
						VolumeType:          pulumi.String("gp2"),
						DeleteOnTermination: pulumi.String("true"),
					},
				},
			},
			IamInstanceProfile: &ec2.LaunchTemplateIamInstanceProfileArgs{
				Name: windowsInstanceProfile.Name,
			},
			ImageId:      pulumi.String(windowsAMI.Value),
			InstanceType: pulumi.String(EksConfig.WindowsNodegroups[key]["instanceType"]),
			KeyName:      pulumi.String(EksConfig.WindowsNodegroups[key]["sshKey"]),
			VpcSecurityGroupIds: pulumi.StringArray{
				windowsNodegroupSg.ID().ToStringOutput(),
			},
			MetadataOptions: &ec2.LaunchTemplateMetadataOptionsArgs{
				HttpEndpoint:            pulumi.String("enabled"),
				HttpTokens:              pulumi.String("optional"),
				HttpPutResponseHopLimit: pulumi.Int(2),
				InstanceMetadataTags:    pulumi.String("disabled"),
			},
			TagSpecifications: ec2.LaunchTemplateTagSpecificationArray{
				&ec2.LaunchTemplateTagSpecificationArgs{
					ResourceType: pulumi.String("instance"),
					Tags:         pulumi.StringMap(CommonTags),
				},
			},
			UserData: templateb64encoded,
		}, pulumi.DependsOn([]pulumi.Resource{EksOutput.LinuxNodeGroups[0]}))
		errorHandler(err)

		clusterTag := eksCluster.EksCluster.Name().ApplyT(func(name string) string {
			return "k8s.io/cluster-autoscaler/" + name
		}).(pulumi.StringOutput)

		windowsAutoscalingGroup, err := autoscaling.NewGroup(ctx, EksConfig.WindowsNodegroups[key]["name"], &autoscaling.GroupArgs{
			Name:            pulumi.String(EksConfig.WindowsNodegroups[key]["name"]),
			DesiredCapacity: pulumi.Int(desiredSize),
			MaxSize:         pulumi.Int(maxSize),
			MinSize:         pulumi.Int(minSize),
			LaunchTemplate: &autoscaling.GroupLaunchTemplateArgs{
				Id:      windowsLaunchTemplate.ID(),
				Version: pulumi.String(fmt.Sprintf("%v%v", "$", "Latest")),
			},
			VpcZoneIdentifiers: getSubnetIds(subnets),
			InstanceRefresh: &autoscaling.GroupInstanceRefreshArgs{
				Strategy: pulumi.String("Rolling")},
			Tags: autoscaling.GroupTagArray{
				&autoscaling.GroupTagArgs{
					Key:               pulumi.String("Name"),
					Value:             pulumi.String("windows-autoscaling-nodegroup"),
					PropagateAtLaunch: pulumi.Bool(true),
				},
				&autoscaling.GroupTagArgs{
					Key:               clusterNameTag,
					Value:             pulumi.String("owned"),
					PropagateAtLaunch: pulumi.Bool(true),
				},
				&autoscaling.GroupTagArgs{
					Key:               clusterTag,
					Value:             pulumi.String("owned"),
					PropagateAtLaunch: pulumi.Bool(true),
				},
				&autoscaling.GroupTagArgs{
					Key:               pulumi.String("k8s.io/cluster-autoscaler/enabled"),
					Value:             pulumi.String("true"),
					PropagateAtLaunch: pulumi.Bool(true),
				},
			},
		})
		errorHandler(err)

		windowsNodeGroups = append(windowsNodeGroups, windowsAutoscalingGroup)
	}

	return windowsNodeGroups
}
