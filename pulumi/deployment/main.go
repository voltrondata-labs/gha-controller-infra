package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/voltrondata/pulumi-go-modules/AWS/eks"
	"github.com/voltrondata/pulumi-go-modules/AWS/vpc"
)

func errorHandler(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {

	pulumi.Run(func(ctx *pulumi.Context) error {
		var vpcOutput vpc.VpcOutput
		var err error

		//Create the VPC
		vpcOutput, err = vpc.CreateVPC(ctx)
		errorHandler(err)

		//Create the EKS cluster
		_, err = eks.CreateEKSCluster(ctx, vpcOutput.Vpc, vpcOutput.PrivateSubnets)
		errorHandler(err)
		return nil
	})

}
