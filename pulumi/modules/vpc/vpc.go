package vpc

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"github.com/voltrondata/pulumi-go-modules/shared/utilities"
)

func errorHandler(err error) {
	if err != nil {
		panic(err)
	}
}

type VpcConfig struct {
	Name             string
	CidrBlock        string
	PrivateSubnets   []string
	PrivateSubnetsAZ []string
	PublicSubnets    []string
	PublicSubnetsAZ  []string
	NatGatewayPerAZ  bool
	Tags             map[string]string
}

type VpcOutput struct {
	Vpc            *ec2.Vpc
	PublicSubnets  []*ec2.Subnet
	PrivateSubnets []*ec2.Subnet
}

func CreateVPC(ctx *pulumi.Context) (VpcOutput, error) {

	// Get the VPC config from context
	VpcConfig := &VpcConfig{}
	conf := config.New(ctx, "")
	conf.RequireObject("Vpc", &VpcConfig)

	// Create a pulumiStringMap for the Tags
	CommonTags := pulumi.StringMap{}
	for index, tag := range VpcConfig.Tags {
		CommonTags[index] = pulumi.String(tag)
	}
	// Initialize the output struct
	VpcOutput := &VpcOutput{}
	// Create a new VPC
	vpcTags := addNameToCommonTags(VpcConfig.Name+"-vpc", CommonTags)
	VPC, err := ec2.NewVpc(ctx, "VPC", &ec2.VpcArgs{
		CidrBlock: pulumi.String(VpcConfig.CidrBlock),
		Tags:      pulumi.StringMap(vpcTags),
	})

	errorHandler(err)
	// Add the VPC to the output Struct
	VpcOutput.Vpc = VPC

	// Create the Internet Gateway
	igwTags := addNameToCommonTags(VpcConfig.Name+"-igw", CommonTags)
	igw, err := ec2.NewInternetGateway(ctx, "igw", &ec2.InternetGatewayArgs{
		VpcId: VPC.ID(),
		Tags:  pulumi.StringMap(igwTags),
	})
	errorHandler(err)

	// Private subnet

	// Create the private subnets

	for index, availabilityZone := range VpcConfig.PrivateSubnetsAZ {

		// Create the Internet Gateway
		subnetTags := addNameToCommonTags(VpcConfig.Name+fmt.Sprintf("-private-subnet-0%d", index), CommonTags)
		subnetArgs := &ec2.SubnetArgs{
			VpcId:               VPC.ID(),
			CidrBlock:           pulumi.String(VpcConfig.PrivateSubnets[index]),
			MapPublicIpOnLaunch: pulumi.Bool(false),
			AvailabilityZone:    pulumi.String(availabilityZone),
			Tags:                pulumi.StringMap(subnetTags),
		}

		subnet, err := ec2.NewSubnet(ctx, fmt.Sprintf("private-subnet-0%d", index), subnetArgs)
		errorHandler(err)

		VpcOutput.PrivateSubnets = append(VpcOutput.PrivateSubnets, subnet)
		ctx.Export(fmt.Sprintf("private-subnet-0%d", index), subnet.ID())
	}

	// Public subnets

	//Create the public subnets
	for index, availabilityZone := range VpcConfig.PublicSubnetsAZ {

		subnetTags := addNameToCommonTags(VpcConfig.Name+fmt.Sprintf("-public-subnet-0%d", index), CommonTags)
		subnetArgs := &ec2.SubnetArgs{
			VpcId:               VPC.ID(),
			CidrBlock:           pulumi.String(VpcConfig.PublicSubnets[index]),
			MapPublicIpOnLaunch: pulumi.Bool(false),
			AvailabilityZone:    pulumi.String(availabilityZone),
			Tags:                pulumi.StringMap(subnetTags),
		}

		subnet, err := ec2.NewSubnet(ctx, fmt.Sprintf("public-subnet-0%d", index), subnetArgs)
		errorHandler(err)
		VpcOutput.PublicSubnets = append(VpcOutput.PublicSubnets, subnet)
		ctx.Export(fmt.Sprintf("public-subnet-0%d", index), subnet.ID())
	}

	var natGatewayID []pulumi.IDOutput
	// Create the nat gateway, private route tables and private route tables association

	// If HA on NatGateways is desired, one nat gateway is created per AZ
	// its mandatory that AZ <= public subnets
	if VpcConfig.NatGatewayPerAZ {

		// for len, publicSubnetAZ := range VpcConfig.PublicSubnetsAZ{
		for index := 0; index < len(VpcConfig.PublicSubnetsAZ); index++ {
			// Create the EIP
			eipTags := addNameToCommonTags(VpcConfig.Name+fmt.Sprintf("-eip-%d", index), CommonTags)
			eip, err := ec2.NewEip(ctx, fmt.Sprintf("eip-%d", index), &ec2.EipArgs{
				Vpc:  pulumi.Bool(true),
				Tags: pulumi.StringMap(eipTags),
			})
			errorHandler(err)
			natGatewayTags := addNameToCommonTags(VpcConfig.Name+fmt.Sprintf("-nat-gateway-%d", index), CommonTags)
			natGateway, err := ec2.NewNatGateway(ctx, fmt.Sprintf("nat-gateway-%d", index), &ec2.NatGatewayArgs{
				AllocationId: eip.AllocationId,
				SubnetId:     VpcOutput.PublicSubnets[index].ID(),
				Tags:         pulumi.StringMap(natGatewayTags),
			})
			errorHandler(err)
			natGatewayID = append(natGatewayID, natGateway.ID())
		}
		// Otherwise, only one NAT Gateway is created
	} else {
		// Create the EIP
		eipTags := addNameToCommonTags(VpcConfig.Name+"-eip", CommonTags)
		eip, err := ec2.NewEip(ctx, "eip", &ec2.EipArgs{
			Vpc:  pulumi.Bool(true),
			Tags: pulumi.StringMap(eipTags),
		})
		errorHandler(err)
		natGatewayTags := addNameToCommonTags(VpcConfig.Name+"-nat-gateway", CommonTags)
		natGateway, err := ec2.NewNatGateway(ctx, "nat-gateway", &ec2.NatGatewayArgs{
			AllocationId: eip.AllocationId,
			SubnetId:     VpcOutput.PublicSubnets[0].ID(),
			Tags:         pulumi.StringMap(natGatewayTags),
		}, pulumi.DependsOn([]pulumi.Resource{eip}))
		errorHandler(err)
		natGatewayID = append(natGatewayID, natGateway.ID())
	}

	// One private  RT per NAT Gateway
	var privateRT []pulumi.IDOutput
	for index, natgateway := range natGatewayID {

		privateRtTags := addNameToCommonTags(VpcConfig.Name+fmt.Sprintf("-private-rt-0%d", index), CommonTags)
		privateRt, err := ec2.NewRouteTable(ctx, fmt.Sprintf("private-rt-%d", index), &ec2.RouteTableArgs{
			VpcId: VPC.ID(),
			Routes: ec2.RouteTableRouteArray{
				&ec2.RouteTableRouteArgs{
					// Connect to the internet through the Internet Gateway
					// If the IP is not within the CIDR block range of the private subnet
					CidrBlock:    pulumi.String("0.0.0.0/0"),
					NatGatewayId: natgateway,
				},
			},
			Tags: pulumi.StringMap(privateRtTags),
		})
		errorHandler(err)
		privateRT = append(privateRT, privateRt.ID())

	}

	// Private subnet route table association:
	// Each RT is assigned to private subnet until there are no more routing tables, then we assign first one
	// This solves for both multi AZ Nat Gateway and single Nat Gateway
	for index, privatesubnetids := range VpcOutput.PrivateSubnets {
		if index >= len(privateRT) {
			_, err = ec2.NewRouteTableAssociation(ctx, fmt.Sprintf("private-subnet-rt-assoc-0%d", index), &ec2.RouteTableAssociationArgs{
				RouteTableId: privateRT[0],
				SubnetId:     privatesubnetids.ID(),
			})
			errorHandler(err)
		} else {
			_, err = ec2.NewRouteTableAssociation(ctx, fmt.Sprintf("private-subnet-rt-assoc-0%d", index), &ec2.RouteTableAssociationArgs{
				RouteTableId: privateRT[index],
				SubnetId:     privatesubnetids.ID(),
			})
			errorHandler(err)
		}
	}

	// Create the public route table
	publicRtTags := addNameToCommonTags(VpcConfig.Name+"-public-rt", CommonTags)
	publicRt, err := ec2.NewRouteTable(ctx, "public-rt", &ec2.RouteTableArgs{
		VpcId: VPC.ID(),
		Routes: ec2.RouteTableRouteArray{
			&ec2.RouteTableRouteArgs{
				// Connect to the internet through the Internet Gateway
				// If the IP is not within the CIDR block range of the VPC
				CidrBlock: pulumi.String("0.0.0.0/0"),
				GatewayId: igw.ID(),
			},
		},
		Tags: pulumi.StringMap(publicRtTags),
	})
	errorHandler(err)

	// Create the public subnet <==> route table association
	for index, publicsubnetids := range VpcOutput.PublicSubnets {
		_, err = ec2.NewRouteTableAssociation(ctx, fmt.Sprintf("public-subnet-rt-assoc-0%d", index), &ec2.RouteTableAssociationArgs{
			RouteTableId: publicRt.ID(),
			SubnetId:     publicsubnetids.ID(),
		})
		errorHandler(err)

	}

	// Create one string array with all the route tables (including private and public), so they can be assigned to the S3 VPC gateway endpoint
	routeTables := utilities.IdOutputArrayToStringOutputArray(privateRT)
	routeTables = append(routeTables, publicRt.ID())

	// Create the VPC endpoint to S3 (Gateway).
	// This should be added by default since it has no cost associated and it makes regional s3 data transfer internal and free
	vpcEndpointTags := addNameToCommonTags(VpcConfig.Name+"-vpc-s3-endpoint", CommonTags)
	_, err = ec2.NewVpcEndpoint(ctx, "s3-vpc-gateway-endpoint", &ec2.VpcEndpointArgs{
		VpcId:         VPC.ID(),
		ServiceName:   pulumi.String("com.amazonaws." + conf.Require("region") + ".s3"),
		RouteTableIds: routeTables,
		Tags:          pulumi.StringMap(vpcEndpointTags),
	})

	errorHandler(err)

	ctx.Export("vpc", VPC.ID())
	ctx.Export("igw-id", igw.ID())

	return *VpcOutput, nil
}

func addNameToCommonTags(name string, commonTags pulumi.StringMap) pulumi.StringMap {
	tagsWithName := pulumi.StringMap{}
	for k, v := range commonTags {
		tagsWithName[k] = v
	}
	tagsWithName["Name"] = pulumi.String(name)
	return tagsWithName
}
