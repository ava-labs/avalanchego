# bootstrap-monitor

Code rooted at this package implements a `bootstrap-monitor` binary
intended to enable continous bootstrap testing for avalanchego
networks.

## Bootstrap testing

Bootstrapping an avalanchego node on a persistent network like
`mainnet` or `fuji` requires that the version of avalanchego that the
node is running be compatible with the historical data of that
network. Bootstrapping regularly is a good way of insuring against
regressions in compatibility.

### Types of bootstrap testing for C-Chain

#### State Sync

A bootstrap with state sync enabled (the default) ensures that only
recent blocks will be processed.

#### Full Sync

All history will be processed, though with pruning (enabled by
default) not all history will be stored.

To enable, supply `state-sync-enabled: false` as C-Chain configuration.

## Architecture TODO(marun) Rename

The intention of `bootstrap-monitor` is to enable a statefulset to
perform continous bootstrap testing for a given avalanchego
configuration. It ensures that a new testing pod either starts or
resumes a test, and upon completion of a test, polls for a new image
to test and initiates a new test when one is found.

 - `bootstrap-monitor init` intended to run as init container of an avalanchego node
   - mounts the same data volume
   - if the image of the avalanchego container is tagged with `latest`
     - runs an avalanchego pod with `latest` to retrieve the
       associated image id
       - not possible to retrieve the image id from the running pod
         since it won't be populated until after the init container
         has finished execution
     - updates the managing stateful set with the image id
   - attempts to read an image name from a file on the data volume
   - if the file is not present or the image name differs from the image name of the pod
     - write the image name to a file on the data volume
     - clear the data volume
     - report that a new bootstrap test is starting for the current image
   - if the image name was present on disk and differs from the current image
     - report that a bootstrap test is being resumed
 - `bootstrap-monitor wait-for-completion` is intended to run as a
   sidepod of the avalanchego container and mount the same data volume read-only
   - every health check interval
     - checks the health of the node
     - logs the disk usage of the data volume
   - once the node is healthy
     - every image check interval
       - starts a pod with the avalanchego image tagged `latest` to find a new image to test
     - once a new image is found
       - updates the managing stateful set with the new image to prompt a new bootstrap test

## Package details

| Filename        | Purpose                                                        |
|:----------------|:---------------------------------------------------------------|
| common.go       | Defines code common between init and wait                      |
| init.go         | Defines how a bootstrap test is initialized                    |
| wait.go         | Defines the loop that waits for completion of a bootstrap test |
| cmd/main.go     | The binary entrypoint for the bootstrap-monitor                |
| e2e/e2e_test.go | The e2e test that validates the bootstrap-monitor              |

## Supporting files

| Filename                                 | Purpose                                           |
|:-----------------------------------------|:--------------------------------------------------|
| scripts/build_bootstrap_monitor.sh       | Builds the bootstrap-monitor binary               |
| scripts/build_bootstrap_monitor_image.sh | Builds the image for the bootstrap-monitor        |
| scripts/tests.e2e.bootstrap_monitor.go   | Script for running the bootstrap-monitor e2e test |

 - The test script is used by the primary github action workflow to
   validate the `bootstrap-monitor` binary and image.
 - The image build script is used by the github action workflow that
   publishes repo images post-merge.

## Alternatives considered

### Run bootstrap tests on hosted github workers

 - allow triggering / reporting to happen with github
   - but 5 day limit on job duration wouldn't probably wouldn't support full sync testing

### Adding a 'bootstrap mode' to avalanchego
 - with a --bootstrap-mode flag, exit on successful bootstrap
   - but using it without a controller would require using `latest` to
     ensure that the node version could change on restarts
   - but when using `latest` there is no way to avoid having pod
     restart preventing the completion of an in-process bootstrap
     test. Only by using a specific image tag will it be possible for
     a restarted pod to reliably resume a bootstrap test.
