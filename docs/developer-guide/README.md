## Development Guide
This document is intended to be the canonical source of truth for things like supported toolchain versions for building Voyager.
If you find a requirement that this doc does not capture, please submit an issue on github.

This document is intended to be relative to the branch in which it is found. It is guaranteed that requirements will change over time
for the development branch, but release branches of Voyager should not change.

### Build Voyager
Some of the Voyager development helper scripts rely on a fairly up-to-date GNU tools environment, so most recent Linux distros should
work just fine out-of-the-box.

#### Setup GO
Voyager is written in Google's GO programming language. Currently, Voyager is developed and tested on **go 1.9.1**. If you haven't set up a GO
development environment, please follow [these instructions](https://golang.org/doc/code.html) to install GO.

#### Download Source

```console
$ go get -u -v github.com/appscode/voyager
$ cd $(go env GOPATH)/src/github.com/appscode/voyager
```

#### Install Dev tools
To install various dev tools for Voyager, run the following command:
```console
$ ./hack/builddeps.sh
```

#### Updating Codes
voyager usages codecgen to generate codes related to kubernetes. If changes happens to api types, codes needs to be regenerated.
API types needs to be updated in both `apis/voyager/v1beta1` and `apis/voyager`. Then run the following command to generate codes:
```console
$ ./hack/codegen.sh
```

#### Build Binary
```
$ ./hack/make.py
$ voyager version
```

#### Dependency management
Voyager uses [Glide](https://github.com/Masterminds/glide) to manage dependencies. Dependencies are already checked in the `vendor` folder.
If you want to update/add dependencies, run:
```console
$ glide slow
```

#### Build Docker images
To build and push your custom Docker image, follow the steps below. To release a new version of Voyager, please follow the [release guide](/docs/developer-guide/release.md).

```console
# Build Docker image
$ ./hack/docker/voyager/setup.sh

# Add docker tag for your repository
$ docker tag appscode/voyager:<tag> <image>:<tag>

# Push Image
$ docker push <image>:<tag>
```

#### Build HAProxy
We package HAProxy and [Kloader](https://github.com/appscode/kloader) into a Ubuntu 16.04 based Docker image.
```console
$ ./hack/docker/haproxy/<version>/setup.sh
$ ./hack/docker/haproxy/<version>/setup.sh push
```

#### Generate CLI Reference Docs
```console
$ ./hack/gendocs/make.sh 
```

### Run Test
#### Run Short Unit Test by running
```console
go test ./pkg/...
```

#### Run Full Test
To Run Full unit test You need to provide some secret in `hack/configs/.env` file. Or You may add them as
environment variables.
```console
TEST_GCE_SERVICE_ACCOUNT_DATA
TEST_GCE_PROJECT
TEST_ACME_USER_EMAIL
TEST_DNS_DOMAINS
```
Then run
```console
$ ./hack/make.py test unit
```

#### Run e2e Test
```
$ ./hack/make.py test minikube # Run Test against minikube, this requires minikube to be set up and started.

$ ./hack/make.py test e2e -cloud-provider=gce # Test e2e against gce cluster

$ ./hack/make.py test integration -cloud-provider=gce # Run Integration test against gce
                                                      # This requires voyager to be deployed in the cluster.

```

```
- Run only matching tests e2e test
$ ./hack/make.py test e2e -cloud-provider=gce -ginkgo.focus=<regexp>


- Run tests but do not delete resource that are created
$ ./hack/make.py test minikube -cloud-provider=gce -cleanup=false


- Run Service IP Persist test with provided IP
$ ./hack/make.py test e2e -cloud-provider=gce -lb-ip=35.184.104.215

```

Tests are run only in namespaces prefixed with `test-`. So, to run tests in your desired namespace, follow these steps:
```
# create a Kubernetes namespace in minikube with
kubectl create ns test-<any-name-you-want>

# run tests
./hack/make.py test minikube -namespace test-<any-name-you-want>
```

#### Full Spectrum of test configs
Following configurations can be enabled for test via flags in `./hack/make.py test`.

| Flag Name | Default | Description |
|-----------|---------|-------------|
| cloud-provider | | Name of cloud Provider |
| ingress-class | | | Ingress class handled by voyager. Unset by default. Set to voyager to only handle ingress with annotation kubernetes.io/ingress.class=voyager. |
| namespace | test- <random> | Run tests in this namespaces |
| haproxy-image| appscode/haproxy:1.7.9-4.0.0-rc.16 | HAProxy image name to run |
| cleanup | true | Turn off cleanup for dynamically generated pods and configmaps. Helps with manual testing |
| in-cluster | false | Operator is running inside cluster. Helps with running operator testing. |
| daemon-host-name | master | Daemon host name to run daemon hosts |
| lb-ip| Check load balancer IP with Static IP address | LoadBalancer persistent IP |
| rbac| false | Cluster have RBAC enabled. |
| cert | false | Run tests regarding certificates |
| dump | os.TempDir() | Dump all Certificates and CA files for TLS ingress tests |

**e2e** tests are powered by [ginkgo](http://onsi.github.io/ginkgo/). All the [configs and flags](https://github.com/onsi/ginkgo/blob/master/config/config.go#L64) of ginkgo are also available.

## Architecture
Voyager works by implementing third party resource data watcher for kubernetes. It connects with k8s apiserver
for specific events as ADD, UPDATE and DELETE. and perform required operations.

Ingress watcher generates the configuration for HAProxy and stores it as a ConfigMaps and creates a RC with
specified HAProxy - that is configured with auto reload while any changes happens to ConfigMap data. This is handled via
[kloader](https://github.com/appscode/kloader). Voyager keeps the ingress resource and the configuration in sync
by performing processing on the resources.

Certificate watcher watch and process certificates third party data and obtain a ACME certificates.


### Third Party Resources
`voyager` depends on two Custom Resource Definition Object `ingress.voyager.appscode.com` and `certificate.voyager.appscode.com`. Those two objects
can be created using following data.

```yaml
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: certificates.voyager.appscode.com
  labels:
    app: voyager
spec:
  group: voyager.appscode.com
  names:
    kind: Certificate
    listKind: CertificateList
    plural: certificates
    shortNames:
    - cert
    singular: certificate
  scope: Namespaced
  version: v1beta1
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: ingresses.voyager.appscode.com
  labels:
    app: voyager
spec:
  group: voyager.appscode.com
  names:
    kind: Ingress
    listKind: IngressList
    plural: ingresses
    shortNames:
    - ing
    singular: ingress
  scope: Namespaced
  version: v1beta1
```

```console
# Create Third Party Resources
$ kubectl apply -f https://raw.githubusercontent.com/appscode/voyager/5.0.0-rc.3/apis/voyager/v1beta1/crds.yaml
```
