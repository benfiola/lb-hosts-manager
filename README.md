# lb-hosts-manager

This utility synchronizes a local hosts file with `LoadBalancer` services deployed to a cluster.  Each exposed load balancer service is named to match their cluster-internal DNS name.  This enables local development and testing code to access in-cluster resources via in-cluster DNS names - despite running out-of-cluster.

NOTE: This _only_ works if the IP addresses assigned to `LoadBalancer` services are reachable from the local machine (e.g., via a `kind` cluster using `cloud-provider-kind` as a load balancer provider).

## Usage

```shell
# ensure KUBECONFIG is set or --kubeconfig is provided
lb-hosts-manager run [--kubeconfig <path/to/kubeconfig>]

# prints utility version
lb-hosts-manager version
```

## Development

I personally use [vscode](https://code.visualstudio.com/) as an IDE. For a consistent development experience, this project is also configured to utilize [devcontainers](https://containers.dev/). If you're using both - and you have the [Dev Containers extension](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers) installed - you can follow the [introductory docs](https://code.visualstudio.com/docs/devcontainers/tutorial) to quickly get started.

### Creating a development environment

From the project root, run the following to create a development cluster to test the operator with:

```shell
cd /workspaces/lb-hosts-manager
make create-cluster
```

This will:

- Download required tools to the _.dev_ folder.
- Create a new dev kind cluster
- Apply testing manifests
- Run `cloud-provider-kind` in the background

### Creating a launch script

Copy the [./dev/dev.go.template](./dev/dev.go.template) script to `./dev/dev.go`, then run it to start the operator against the local development environment. `./dev/dev.go` is gitignored and you can change this file as needed without worrying about committing it to git. Additionally, the devcontainer is configured with vscode launch configurations that point to this file. You should be able to launch (and attach a debugger to) the operator by launching it natively through vscode.
