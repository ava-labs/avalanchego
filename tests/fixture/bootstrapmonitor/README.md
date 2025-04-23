# bootstrap-monitor

Code rooted at this package implements a `bootstrap-monitor` binary
intended to enable continuous bootstrap testing for avalanchego
networks.

## Bootstrap testing

Bootstrapping an avalanchego node on a persistent network like
`mainnet` or `fuji` requires that the version of avalanchego that the
node is running be compatible with the historical data of that
network. Bootstrapping regularly is a good way of insuring against
regressions in compatibility.

### Sync modes for bootstrap testing

The 'sync mode' for a bootstrap test determines what history will be
processed (fetched and executed) during the bootstrap process of a
non-validating node. There are 3 such modes:

#### C-Chain State Sync

In this mode, the full history of the X- and P-Chains will
processed. Only recent blocks of the C-Chain will be processed. This
is the default mode.

#### Only P-Chain Full Sync

In this mode, only the full history of the P-Chain will be
processed. This is the expected mode for L1 validators. This mode is
configured by the supplying the `--partial-sync-primary-network`
flag.

#### Full Sync

In this mode, the full history of the X-, P- and C-Chains will be
processed. This is configured by including
`state-sync-enabled:false` in the C-Chain configuration.

## Overview

The intention of `bootstrap-monitor` is to enable a Kubernetes
[StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
to perform continuous bootstrap testing for a given avalanchego
configuration. It ensures that a testing pod either starts or resumes
a test, and upon completion of a test, polls for a new image to test
and initiates a new test when one is found.

 - Both the `init` and `wait-for-completion` commands of the
   `bootstrap-monitor` binary are intended to run as containers of a
   pod alongside an avalanchego container. The pod is expected to be
   managed by a `StatefulSet` to ensure the pod is restarted on
   failure and that only a single pod runs at a time to avoid
   contention for the backing data volume. Both commands derive the
   configuration of a bootstrap test from the pod:
   - The network targeted by the test is determined by the value of
     the `AVAGO_NETWORK_NAME` env var set for the avalanchego
     container.
   - By default, the sync mode will be `c-chain-state-sync`. The other sync
     modes require providing additional configuration:
     - The `only-p-chain-full-sync` mode is enabled by setting the
       `AVAGO_PARTIAL_SYNC_PRIMARY_NETWORK` env var to `"true"` for
       the avalanchego container. If this mode is enabled, the state
       sync configuration for the C-Chain will be ignored.
     - The `full-sync` mode is enabled by including
       `state-sync-enabled:false` in the
       `AVAGO_CHAIN_CONFIG_CONTENT` env var set for the avalanchego
       container and either not including a value for
       `AVAGO_PARTIAL_SYNC_PRIMARY_NETWORK` or setting it to
       `"false"`.
   - The image used by the test is determined by the image configured
     for the avalanchego container.
   - The versions of the avalanchego image used by the test is
     determined by the pod annotation with key
     `avalanche.avax.network/avalanchego-versions`.
 - When a bootstrap testing pod is inevitably rescheduled or
   restarted, the contents of the `PersistentVolumeClaim` configured
   by the managing `StatefulSet` will persist across pod restarts to
   allow resumption of the interrupted test.
 - Both the `init` and `wait-for-completion` commands of the
   `bootstrap-monitor` attempt to read serialized test details (namely
   the image used for the test and the start time of the test) from
   the same data volume used by the avalanchego node. These details
   are written by the `init` command when it determines that a new test
   is starting.
 - The `bootstrap-monitor init` command is intended to run as an
   [init
   container](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/)
   of an avalanchego node and ensure that the ID of the image and its
   associated versions are recorded for the test and that the contents
   of the pod's data volume is either cleared for a new test or
   retained to enable resuming a previously started test. It
   accomplishes this by:
   - Mounting the same data volume as the avalanchego node
   - Reading bootstrap test configuration as described previously
   - Determining the image ID and versions for an image if the
     avalanchego image for the pod uses the `latest` tag. This will
     only need to be performed the first pod that a bootstrap testing
     `StatefulSet` runs. Subsequent pods from the same `StatefulSet`
     should have an image qualified with its SHA and version details
     set by the previous test run's `wait-for-completion` pod.
     - A new pod will be started with the `latest` image to execute
     `avalanchego --versions-json` to determine the image ID (which
     includes a sha256 hash) of the image and its avalanchego
     versions. Those values will then be applied to the `StatefulSet`
     managing the pod which will prompt pod deletion and recreation
     with the updated values. This ensures that a test result can be
     associated with both a specific image SHA and the avalanchego
     versions (including commit hash) of the binary that the image
     provides.
     - A separate pod is used because the image ID of a non-init
       avalanchego container using a `latest`-tagged image is only
       available when that container runs rather than when an init container runs.
     - While it would be possible to add an init container running the
       same avalanchego image as the primary avalanchego container,
       have it run the version command, and then have a subsequent
       `bootstrap-monitor init` container read those results, the use
       of a separate pod for SHA and versions discovery would still be
       required by the `wait-for-completion` command. It seemed
       preferable to have only a single way to discover image details.
   - Attempting to read the serialized test details from a file on the
     data volume. This file will not exist if the data volume has not
     been used before.
   - Comparing the image from the serialized test details to the image
     in the test configuration.
     - If the images differ (or the file was not present), the data
       volume is initialized for a new test:
       - The data volume is cleared
       - The image from the test configuration and the time are
         serialized to a file on the data volume
     - If the images are the same, the data volume is used as-is to
       enable resuming an in-progress test.
 - `bootstrap-monitor wait-for-completion` is intended to run as a
   sidecar of the avalanchego container. It polls the health of the
   node container to detect when a bootstrap test has completed
   successfully and then polls for a new image to test. When a new
   image is found, the managing `StatefulSet` is updated with the
   details of the image to trigger a new test. The process to detect a
   new image is the same as was described for the `init` command.

## Package details

| Filename                 | Purpose                                                                                     |
|:-------------------------|:--------------------------------------------------------------------------------------------|
| bootstrap_test_config.go | Defines how the configuration for a bootstrap test is read from a pod                      |
| common.go                | Defines code common between init and wait                                                   |
| init.go                  | Defines how a bootstrap test is initialized                                                 |
| wait.go                  | Defines how a bootstrap test is determined to have completed and how a new one is initiated |
| cmd/main.go              | The binary entrypoint for the `bootstrap-monitor`                                           |
| e2e/e2e_test.go          | The e2e test that validates `bootstrap-monitor`                                             |

## Supporting files

| Filename                                 | Purpose                                           |
|:-----------------------------------------|:--------------------------------------------------|
| scripts/build_bootstrap_monitor.sh       | Builds the `bootstrap-monitor` binary               |
| scripts/build_bootstrap_monitor_image.sh | Builds the image for the `bootstrap-monitor`        |
| scripts/tests.e2e.bootstrap_monitor.go   | Script for running the `bootstrap-monitor` e2e test |

 - The test script is used by the github action workflow that
   validates the `bootstrap-monitor` binary and image.
 - The image build script is used by the github action workflow that
   publishes repo images post-merge.

## Alternatives considered

### Run bootstrap tests on github workers

 - Public github workers are not compatible with bootstrap testing due
to the available storage of 30GB being insufficient for even state
sync bootstrap.
 - Self-hosted github workers are not compatible with bootstrap testing
due to the 5-day maximum duration for a job running on a self-hosted
runner. State sync bootstrap usually completes within 5 days, but full
sync bootstrap usually takes much longer.

### Adding a 'bootstrap mode' to avalanchego

If avalanchego supported a `--bootstrap-mode` flag that exited on
successful bootstrap, and a pod configured with this flag used an
image with a `latest` tag, the pod would continuously bootstrap, exit,
and restart with the current latest image. While appealingly simple,
this approach doesn't directly support:

 - A mechanism for resuming a long-running bootstrap. Given the
expected duration of a bootstrap test, and the fact that a workload on
Kubernetes is not guaranteed to run without interruption, a separate
init process is suggested to enable resumption of an interrupted test.
- A mechanism for reporting disk usage and duration of execution
