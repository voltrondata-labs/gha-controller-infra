# EKS MODULE

This module can create EKS clusters with both Linux EKS-managed node groups and Windows self-managed node groups.

## Requisites

The module has to be called passing the VPC to be used (*ec2.Vpc) and the list of subnets where the cluster will be created.  

```
CreateEKSCluster(ctx *pulumi.Context, vpc *ec2.Vpc, subnets []*ec2.Subnet) (EksOutput, error)
```
Also, it requires some configurations. Find below an example of a config file. 

```
  arrowci:Eks:
    Name: "arrow-self-hosted-runners"
    Version: "1.23"
    LinuxNodegroups:
      nodegroup1:
        name: "linux-nodegroup"
        minSize: "0"
        maxSize: "3"
        desiredSize: "0"
        diskSize: "50"
        instanceType: "m5.large"
        amiType: "AL2_x86_64"
        sshKey: "arrowCI"
    WindowsNodegroups:
      nodegroup1:
        name: "windows-nodegroup"
        minSize: "0"
        maxSize: "2"
        desiredSize: "0"
        diskSize: "80"
        instanceType: "m5.large"
        sshKey: "arrowCI"
    tags:
      environment: "development"
```
# Additional steps using windows nodes

Two Config Maps need to be added/updated when creating a cluster with Windows Node Groups.

## aws-auth ConfigMap

Instructions are detailed on this AWS document: https://docs.aws.amazon.com/eks/latest/userguide/launch-windows-workers.html Step 2 of AWS Management Console instructions tab 


Download the configuration map:
```
curl -o aws-auth-cm-windows.yaml https://s3.us-west-2.amazonaws.com/amazon-eks/cloudformation/2020-10-29/aws-auth-cm-windows.yaml
```

Open the file using your preferred text editor. Replace the ARN of the instance role (not instance profile) of the **Linux** node and the ARN of the instance role (not instance profile) of **Windows** node snippets with the NodeInstanceRole values that you recorded for your Linux and Windows nodes, and save the file.

Important
Don't modify any other lines in this file.

Don't use the same IAM role for both Windows and Linux nodes.
```
apiVersion: v1
kind: ConfigMap
metadata:
  name: aws-auth
  namespace: kube-system
data:
  mapRoles: |
    - rolearn: ARN of instance role (not instance profile) of **Linux** node
      username: system:node:{{EC2PrivateDNSName}}
      groups:
        - system:bootstrappers
        - system:nodes
    - rolearn: ARN of instance role (not instance profile) of **Windows** node
      username: system:node:{{EC2PrivateDNSName}}
      groups:
        - system:bootstrappers
        - system:nodes
        - eks:kube-proxy-windows
```
Apply the configuration. This command might take a few minutes to finish.
```
kubectl apply -f aws-auth-cm-windows.yaml
```

## amazon-vpc-cni ConfigMap

Instructions are detailed in this AWS document: https://docs.aws.amazon.com/eks/latest/userguide/windows-support.html Enabling Windows support steps 3 and 4

Create a file named `vpc-resource-controller-configmap.yaml` with the following contents.
```
apiVersion: v1
kind: ConfigMap
metadata:
  name: amazon-vpc-cni
  namespace: kube-system
data:
  enable-windows-ipam: "true"
```
Apply the ConfigMap to your cluster.
```
kubectl apply -f vpc-resource-controller-configmap.yaml
```

# Auto Scaling

## Module Resources

This module provisions the necessary IAM resources to deploy the cluster Autoscaler. This Autoscaler enables horizontal scaling of the nodes based on usage metrics within the cluster. Currently,
it provisions: 

- IAM role
- IAM policies
- OIDC provider for Role-Service Account connection

However, it doesn't deploy the cluster Autoscaler as that is a Cluster-level deployment and not an infrastructure one.

## Next Steps - Helm & FluxCD

The next step to enable the Autoscaler is to deploy it in the cluster. We do this through the implementation of the Helm chart deployment (https://github.com/kubernetes/autoscaler/tree/master/charts/cluster-autoscaler); however, in the DevOps team we deploy Helm charts through the use of FluxCD. This would mean deploying the Helm chart as a Helm Release through the use of FluxCD. In the link above you can see the values needed to configure the chart and have it manage the cluster's autoscaling features.
