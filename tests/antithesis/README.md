# Antithesis Testing

This package supports testing with
[Antithesis](https://antithesis.com/docs/introduction/how_antithesis_works),
a SaaS offering that enables deployment of distributed systems (such
as Avalanche) to a deterministic and simulated environment that
enables discovery and reproduction of anomalous behavior.

## Package details

| Filename                          | Purpose                                                                            |
|:----------------------------------|:-----------------------------------------------------------------------------------|
| compose.go                        | Generates Docker Compose project file and initial database for antithesis testing. |
| config.go                         | Defines common flags for the workload binary.                                      |
| Dockerfile.builder-instrumented   | Dockerfile for instrumented builds.                                                |
| Dockerfile.builder-uninstrumented | Dockerfile for uninstrumented builds.                                              |
| config.go                         | Defines common flags for the workload binary.                                      |
| init_db.go                        | Initializes initial db state for subnet testing.                                   |
| node_health.go                    | Helper to check node health.                                                       |
| avalanchego/                      | Defines an antithesis test setup for avalanchego's primary chains.                 |
| subnet-evm/                       | Defines Dockerfiles for the subnet-evm test setup (Go source in graft/subnet-evm). |
| xsvm/                             | Defines an antithesis test setup for the xsvm VM.                                  |

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

**Note on subnet-evm**: The subnet-evm test setup has a hybrid structure, where the Go code is kept in subnet-evm, while the testing scripts are at the root level, to respect module import rules. More specifically, The Dockerfiles are located in `tests/antithesis/subnet-evm/`, but the Go source code (`main.go` and `gencomposeconfig/main.go`) remains in `graft/subnet-evm/tests/antithesis/` because it needs to import from `graft/subnet-evm` packages, which are forbidden from being imported cross module. The root-level build scripts build the binaries from their graft location.

// TODO(JonathanOppenheimer) Once the graft folder has been fully subsumed into `vms/evm`, we can move all the files properly.

### Use of a builder image

To simplify building instrumented (for running in CI) and
non-instrumented (for running locally) versions of the workload and
node images, a common builder image is used. If on an amd64 host,
`tests/antithesis/avalanchego/Dockerfile.builder-instrumented` is used
to create an instrumented builder. On an arm64 host,
`tests/antithesis/avalanchego/Dockerfile.builder-uninstrumented` is
used to create an uninstrumented builder. In both cases, the builder
image is based on the default golang image and will include the source
code necessary to build the node and workload binaries. The
alternative would require duplicating builder setup for instrumented
and non-instrumented builds for the workload and node images of each
test setup.

## Troubleshooting a test setup

### Running a workload with an existing network

The workload of the 'avalanchego' test setup can be invoked against an
arbitrary network:

```bash
$ AVAWL_URIS="http://10.0.20.3:9650 http://10.0.20.4:9650" go run ./tests/antithesis/avalanchego
```

The workload of a subnet test setup like 'xsvm' additionally requires
a network with a configured chain for the xsvm VM and the ID for that
chain needs to be provided to the workload:

```bash
$ AVAWL_URIS=... CHAIN_IDS="2S9ypz...AzMj9" go run ./tests/antithesis/xsvm
```

### Running a workload with a tmpnet network

Just like with e2e tests, running an antithesis workload against a
tmpnet network requires specifying an avalanchego path (either as an
argument or an env var):

```bash
$ go run ./tests/antithesis/avalanchego --avalanchego-path=/path/to/avalanchego
```

All tmpnet flags are supported (e.g. `--reuse-network`,
`--stop-network`, `--restart-network`, `--node-count`).  See the
[tmpnet documentation](../fixture/tmpnet/README.md) for more details.

### Running a workload with docker compose v2

Running the test script for a given test setup with the `DEBUG` flag
set will avoid cleaning up the temporary directory where the
docker compose setup is written to. This will allow manual invocation of
docker compose to see the log output of the workload.

```bash
$ DEBUG=1 ./scripts/tests.build_antithesis_images.sh
```

After the test script has terminated, the name of the temporary
directory will appear in the output of the script:

```
...
using temporary directory /tmp/tmp.E6eHdDr4ln as the docker compose path
...
```

Running compose from the temporary directory will ensure the workload
output appears on stdout for inspection:

```bash
$ cd [temporary directory]

# Start the compose project
$ docker compose up

# Cleanup the compose project
$ docker compose down --volumes
```

## Manually triggering an Antithesis test run

When making changes to a test setup, it may be useful to manually
trigger an Antithesis test run outside of the normal schedule. This
can be performed against master or an arbitrary branch:

 - Navigate to the ['Actions' tab of the avalanchego
   repo](https://github.com/ava-labs/avalanchego/actions).
 - Select the [Publish Antithesis
   Images](https://github.com/ava-labs/avalanchego/actions/workflows/publish_antithesis_images.yml)
   workflow on the left.
 - Find the 'Run workflow' drop-down on the right and trigger the
   workflow against the desired branch. The default value for
   `image_tag` (`latest`) is used by scheduled test runs, so consider
   supplying a different value to avoid interfering with the results
   of the scheduled runs.
 - Wait for the publication job to complete successfully so that the
   images are available to be tested against.
 - Select one of the [Trigger Antithesis Avalanchego
   Setup](https://github.com/ava-labs/avalanchego/actions/workflows/trigger-antithesis-avalanchego.yml)
   or [Trigger Antithesis XSVM
   Setup](https://github.com/ava-labs/avalanchego/actions/workflows/trigger-antithesis-xsvm.yml)
   workflows on the left.
 - Find the 'Run workflow' drop-down on the right and trigger the
   workflow against the desired branch. The branch only determines the
   CI configuration (the images have already been built), so master is
   probably fine. Make sure to supply the same `image_tag` that was
   provided to the publishing workflow and provide a value for
   `recipients` (e.g. your email address) to avoid sending the test
   report to everyone on the regular distribution list.
