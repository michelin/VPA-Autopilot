# VPA Autopilot

`VPA Autopilot` is a Go controller that creates a VPA for the CPU request for each deployment and statefulset in the cluster in order to have the correct resources for pods at all times.

## Description

The controller deploys a VPA by default for all deployments unless there is another autoscaling resource associated to it (A HPA or another VPA), in that case the controller skips the deployment and delete any default VPA previously generated associated to the deployment.

It is possible to specify a list of namespaces that will be ignored by the controller by passing a comma separated list to the parameter `excluded-namespaces`. It is also possible to ignore all namespaces matching a label key/value by setting `excluded-namespaces-key` and `excluded-namespaces-value`. These namespaces will not be reconciled at all.

The generated VPAs all have a common label defiled by the parameters `vpa-label-key` and `vpa-label-value` (by default `vpa-autopilot-managed: true`) and only affect the CPU resource to be compatible with JVM workloads.

Limitations:

- only the default VPA recommander is used
- the memory is not managed by the generated VPA. This is an opiniated choice to avoid bad interractions with certain workloads, such as Java
- the statefulsets are not managed (they will be in a later version of the controller)

## Getting Started

### Prerequisites

- go version v1.25.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster

**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/auto-vpa:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/auto-vpa:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall

**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

    ```sh
    make build-installer IMG=<some-registry>/auto-vpa:tag
    ```

    NOTE: The makefile target mentioned above generates an 'install.yaml'
    file in the dist directory. This file contains all the resources built
    with Kustomize, which are necessary to install this project without
    its dependencies.

1. Using the installer

Users can just run `kubectl apply -f <URL for YAML BUNDLE>` to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/auto-vpa/<tag or branch>/dist/install.yaml
```

## Features left to do

- Handle Statefulsets in another controller, and rework others to not assume a deployment is targeted
- Handle Daemonsets
