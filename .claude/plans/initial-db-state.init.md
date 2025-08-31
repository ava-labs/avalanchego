# Deploying networks with initial db state

## Background

- The `tmpnet` package is located at ../../tests/fixture/tmpnet and enables deploying avalanchego networks for testing purposes
- It supports running nodes either as processes (process_runtime.go) or as kube pods (kube_runtime.go)
- deployment to kube requires:
  - ensuring a local kind cluster is running: `./bin/tmpnetctl start-kind-cluster`
  - starting the network with the kube runtime: `./bin/tmpnetctl start-network --runtime=kube`

## Outline
 - Deployment usually starts with an empty db state
 - Want to support starting with a populated db
   - but only for the kube runtime because the expected db sizes will be big (~150GiB)
 - Suggest updating the start-network command to accept a new flag to configure downloading a large file from s3
   - e.g. start-network --runtime=kube --initial-db-state=s3://[...]
   - This would require
     - updating tmpnet/flags/kube_runtime.go to accept the new flag
     - updating tmpnet/kube_runtime.go to add a new field to KubeRuntimeConfig
 - If this flag is set as part of network bootstrap, before starting any nodes
   - Start a job that downloads the referenced file from S3 to a PVC in the target namespace
     - Assume the file is in gz format
   - Configure the statefulsets for the nodes to each mount the PVC read-only and copy its contents to their /data directory
     - e.g. with an init container
     - The copy should be performed at most once such that restarting a node should not result in an attempt to copy again
       - e.g. by writing a sentinal file when copying is complete and checking for that file before attempting copying
   - Ideally the job and init container can use simple bash scripting rather than requiring a custom image that would need to be maintained
