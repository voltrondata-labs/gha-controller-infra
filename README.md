# GitHub Actions Controller Infrastructure

* [Overview](#overview)
* [Deployment Instructions](#deployment-instructions)
    * [Pre-requisite Software Installation](#pre-requisite-software-installation)
    * [AWS and GitHub Account Setup](#aws-and-github-account-setup)
    * [Deploying the Pulumi setup](#deploying-the-pulumi-setup)
    * [Pre-requisites for Flux and the Actions Runner Controller](#pre-requisites-for-flux-and-the-actions-runner-controller)
    * [Flux setup](#flux-setup)
* [License](#license)

## Overview

The Voltron Data DevOps team has developed a solution to provide an Actions Runner Controller to support GitHub Actions workflows with a Kubernetes cluster that can auto-scale according to the needs of the workflows. It includes both Horizontal pod and node auto-scaling across both Linux and Windows nodes.

## Deployment Instructions

### Pre-requisite Software Installation

> You need to install Pulumi, the Go language runtime, the Flux CLI, the AWS CLI and kubectl in your local environment
>
1. Install Pulumi: [https://www.pulumi.com/docs/get-started/aws/begin/#install-pulumi](https://www.pulumi.com/docs/get-started/aws/begin/#install-pulumi)
2. Install Go: [https://go.dev/doc/install](https://go.dev/doc/install)
3. Install Flux: [https://fluxcd.io/flux/installation/#install-the-flux-cli](https://fluxcd.io/flux/installation/#install-the-flux-cli)
4. Install AWS CLI: [https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
5. Install kubectl: [https://kubernetes.io/docs/tasks/tools/#kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl)

### AWS and GitHub Account Setup

> You need to configure both your local and cloud environments to interact with AWS and GitHub
> 
1. Deploy the S3 bucket we will use for the backend state
    1. There is a CloudFormation template in the `pulumi/backend` folder. Deploy it in the region you want to host the project
2. Create an SSH Key in AWS EC2 and store its name: [https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/create-key-pairs.html](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/create-key-pairs.html)
3. Open a terminal session
    1. Confirm that you can see the S3 bucket created. 
        1. `aws s3 ls`
            
            > Set the profile (`AWS_PROFILE=XXX`) or the region (`AWS_REGION=XXX`), if you are not using the defaults in your profile
            > 
4. Login to your bucket using Pulumi
    1. `pulumi login s3://xxx.xxx.xxx/xxx/xxx`
    2. [https://www.pulumi.com/docs/intro/concepts/state/#aws-s3](https://www.pulumi.com/docs/intro/concepts/state/#aws-s3)
5. Make sure your GitHub account has an SSH Key configured:
    1. [https://github.com/settings/keys](https://github.com/settings/keys)

### Docker & CI/CD

*** ADD SECTION ***

- Update .github/workflows with this repo owner as the first parameter and the repo name as the second parameter.
- Trigger a manual run of each Action.
- Change package visibility

### Deploying the Pulumi setup

> Now that we have our local and cloud environments set up, we can continue with doing the main deployment of the stack
> 
1. Clone the `voltrondata-labs/gha-controller-infra` repo
    1. `git clone git@github.com:voltrondata-labs/gha-controller-infra.git`
2. Update the URL of the Pulumi backend in `Pulumi.yaml` key `backend.url`
3. Deploy a new stack of the Pulumi deployment:
    
    > This will create a new stack
    > 
    1. `cd pulumi/deployment`
    2. `pulumi stack init production`
        1. **This step will create a new file called `Pulumi.production.yaml` which will only have the `encryptionsalt`; copy all of the other values from `Pulumi.staging.yaml` into this file and replace the details as needed. Remember that you need to replace the tags and the SSH Key Name at the least.**
        2. **It will ask you to create a passphrase; store it well as you will need it to make stack updates**
    3. `pulumi up`

### Pre-requisites for Flux and the Actions Runner Controller

1. Create a GitHub App Secret for the Controller
    
    > We need to create a GitHub App in the organization in which the repository that will trigger the Actions lives and store it’s credentials as a secret in the Kubernetes cluster; this is not the same repository as the repository holding the infrastructure code
    > 
    1. Create the organization’s GitHub app with the necessary scopes:
        1. [https://github.com/actions-runner-controller/actions-runner-controller/blob/master/docs/detailed-docs.md#deploying-using-github-app-authentication](https://github.com/actions-runner-controller/actions-runner-controller/blob/master/docs/detailed-docs.md#deploying-using-github-app-authentication)
    2. Install the GitHub App in the organization and give it access to the repository
    3. Follow the document linked above to get the App ID (`APP_ID`), Installation ID (`INSTALLATION_ID`), and the downloaded private key file.
    4. Format the private key file: `openssl pkcs8 -topk8 -inform PEM -outform PEM -in downloaded-key.pem -out new-key.pem -nocrypt`
    5. Set your local `kubectl config` to the cluster you created:
        1. `aws eks update-kubeconfig --region region-code --name my-cluster`
    6. Create the `actions-runner-system` namespace:

        ```
        kubectl create ns actions-runner-system
        ```
    7. Export the variables:

        ```
        export APP_ID=""
        export INSTALLATION_ID=""
        export PRIVATE_KEY_FILE_PATH=""
        ```
        
    8. Create a secret in the cluster to store the credentials (note the name):
        
        ```
        kubectl create secret generic controller-manager \
            -n actions-runner-system \
            --from-literal=github_app_id=${APP_ID} \
            --from-literal=github_app_installation_id=${INSTALLATION_ID} \
            --from-file=github_app_private_key=${PRIVATE_KEY_FILE_PATH}
        ```
        
2. Bootstrapping Flux to the Kubernetes cluster
    
    > Now we need to set up Flux with the Kubernetes cluster to have Continuous Deployment up and running in the cluster. This way we can manage the runners from the GitHub YAML files instead of from the `kubectl` CLI tool.
    > 
    1. Generate a [classic personal access token](https://help.github.com/en/github/authenticating-to-github/creating-a-personal-access-token-for-the-command-line) (PAT) that can create and manage existing repositories by checking all permissions under `repo` and `admin`
        1. This PAT will be to write to the repo hosting the infrastructure. We recommend having a repository for the infrastructure and another repository that will be the one hosting and submitting the GitHub Actions.
    2. Set your PAT: `export GITHUB_TOKEN=<your-token>`
    3. Bootstrap Flux in Production:
        1. `flux bootstrap github --owner=<org> --repository=<repo> --path fluxcd/clusters/production`

### Flux setup

1. Helm Deployments
    1. Copy all of the files in `fluxcd/clusters/staging` into `fluxcd/clusters/production` **except for the files inside the `flux-system` folder**.
    2. Update the `kustomizations.yaml` file to point to the production cluster in the paths of the Kustomizations
    3. You need to replace/fill in values for two deployments:
        1. `aws-system/aws-auth.yaml`
            1. Replace the values for the two role ARNs with the ones from the pulumi output. If you need to get the output again you can run `pulumi stack output` and it will print the values. The first value is for the Linux ARN and the second value is for the Windows ARN.
        2. `aws-system/aws-cluster-autoscaler-autodiscover.yaml`
            1. The value of `annotations.[eks.amazonaws.com/role-arn`  in line 9 should also be replaced by the role ARN of the Cluster Autoscaler in your account. This also shows up in the `pulumi stack output` with the key `autoScalerRoleArn`.
            2. The value of `k8s.io/cluster-autoscaler` in line 168 needs to be replaced with the cluster name

With these steps, you should be successful in deploying an Actions Runner Controller with an Autoscaler enabled ready to receive jobs from the GitHub Actions API.

## License

Copyright [2022] [Voltron Data]

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License. 
