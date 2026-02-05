# Task: Deploy minio

Deploy minio (open source s3) to a local Kuberentes KIND cluster.

This will enable future integration testing of a feature needing to transfer data to and from s3.

## Background

- this repo has a way of deploying a local kind cluster
  - @scripts/nix-develop.sh --command @scripts/bin/tmpnetctl start-kind-cluster
- @tests/fixture/tmpnet/tmpnnetctl/main.go defines tmpnetctl's entrypoint
- The implementation of start-kind-cluster is in @tests/fixture/tmpnet/start_kind_cluster.go
  - Defines how to install tools with helm: nginx ingress controller and optionally, chaos mesh

## Suggested tasks
 - Add optional deployment of minio to the local kind cluster (e.g. --install-mini as an option to start-kind-cluster)
 - Install minio with helm in a similar fashion to the other tools already installed with helm.
   - Ensure that minio configures ingress to expose itself similar to how the chaos mesh dashboard is exposed
 - Devise a simple test involving both uploading and downloading to minio.
