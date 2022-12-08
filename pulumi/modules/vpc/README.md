# VPC MODULE

This module creates an AWS VPC with all it's minimal components (subnets, route tables, internet gateway, nat gateways...)

## Requisites

The module has to be called using the pulumi context

```
CreateVPC(ctx *pulumi.Context) (VpcOutput, error)
```
Also, it requires some configurations:

```
  arrowci:Vpc:
    Name: "arrowci"
    cidrBlock: "10.20.0.0/21"
    privateSubnets: 
      - "10.20.5.0/24"
      - "10.20.6.0/24"
    privateSubnetsAZ:
      - "us-west-2a"
      - "us-west-2b"
    publicSubnets: 
      - "10.20.1.0/24"
      - "10.20.2.0/24"
    publicSubnetsAZ:
      - "us-west-2a"
      - "us-west-2b"
    natGatewayPerAZ: false
    tags:
      environment: "development"
      team: "devops"
      owner: "devops_voltrondata_com"
```
