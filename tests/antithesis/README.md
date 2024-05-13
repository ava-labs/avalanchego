# Antithesis Testing

This package supports testing with
[Antithesis](https://antithesis.com/docs/introduction/introduction.html),
a SaaS offering that enables deployment of distributed systems (such
as Avalanche) to a deterministic and simulated environment that
enables discovery and reproduction of anomalous behavior.

## Package details

| Filename     | Purpose                                                                           |
|:-------------|:----------------------------------------------------------------------------------|
| compose.go   | Enables generation of Docker Compose project files for antithesis testing.        |
| avalanchego/ | Contains resources supporting antithesis testing of avalanchego's primary chains. |


## Instrumentation

Software running in Antithesis's environment must be
[instrumented](https://antithesis.com/docs/instrumentation/overview.html)
to take full advantage of the supported traceability. Since the
Antithesis Go SDK only supports the amd64/x86_64 architecture as of this
writing, running of instrumented binaries on Macs (arm64) is not possible
without emulation (which would be very slow). To support test development
on Macs, a local build will not be instrumented.

## Defining a new test setup

When defining a new test setup - whether in the avalanchego repo or
for a VM in another repo - following the example of an existing test
setup is suggested. The following table enumerates the files defining
a test setup:

| Filename                                               | Purpose                                                |
|:-------------------------------------------------------|:-------------------------------------------------------|
| scripts/build_antithesis_images.sh                     | Builds the test images to deploy to antithesis         |
| scripts/build_antithesis_[test setup]_workload.sh      | Builds the workload binary                             |
| scripts/tests.build_antithesis_images.sh               | Validates the build of the test images                 |
| tests/antithesis/[test setup]/main.go                  | The entrypoint for the workload binary                 |
| tests/antithesis/[test setup]/Dockerfile.config        | Defines how to build the config image                  |
| tests/antithesis/[test setup]/Dockerfile.node          | Defines how to build the instrumented node image       |
| tests/antithesis/[test setup]/Dockerfile.workload      | Defines how to build the workload image                |
| tests/antithesis/[test setup]/gencomposeconfig/main.go | Generates the compose configuration for the test setup |

In addition, github workflows are suggested to ensure
`scripts/tests.build_antithesis_images.sh` runs against PRs and
`scripts/build_antithesis_images.sh` runs against pushes.
