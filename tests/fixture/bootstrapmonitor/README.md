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

## ArgoCD

- will need to ignore differences in the image tags of the statefulsets

## Alternatives considered

### Run bootstrap tests on hosted github workers

 - allow triggering / reporting to happen with github
   - but 5 day limit on job duration wouldn't probably wouldn't support full-sync

### Adding a 'bootstrap mode' to avalanchego
 - with a --bootstrap-mode flag, exit on successful bootstrap
   - but using it without a controller would require using `latest` to
     ensure that the node version could change on restarts
   - but when using `latest` there is no way to avoid having pod
     restart preventing the completion of an in-process bootstrap
     test. Only by using a specific image tag will it be possible for
     a restarted pod to reliably resume a bootstrap test.

## Terminology


 -
### Full Sync

### Pruning

### State Sync
