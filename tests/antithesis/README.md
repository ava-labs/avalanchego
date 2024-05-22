# Antithesis Testing

This package supports testing with
[Antithesis](https://antithesis.com/docs/introduction/introduction.html),
a SaaS offering that enables deployment of distributed systems (such
as Avalanche) to a deterministic and simulated environment that
enables discovery and reproduction of anomalous behavior.

## Package details

| Filename       | Purpose                                                                            |
|:---------------|:-----------------------------------------------------------------------------------|
| compose.go     | Generates Docker Compose project file and initial database for antithesis testing. |
| config.go      | Defines common flags for the workload binary.                                      |
| init_db.go     | Initializes initial db state for subnet testing.                                   |
| node_health.go | Helper to check node health.                                                       |
| avalanchego/   | Defines an antithesis test setup for avalanchego's primary chains.                 |
| xsvm/          | Defines an antithesis test setup for the xsvm VM.                                  |

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

## Troubleshooting a test setup

### Running a workload directly

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

### Running a workload with docker-compose

Running the test script for a given test setup with the `DEBUG` flag
set will avoid cleaning up the the temporary directory where the
docker-compose setup is written to. This will allow manual invocation of
docker-compose to see the log output of the workload.

```bash
$ DEBUG=1 ./scripts/tests.build_antithesis_images.sh
```

After the test script has terminated, the name of the temporary
directory will appear in the output of the script:

```
...
using temporary directory /tmp/tmp.E6eHdDr4ln as the docker-compose path"
...
```

Running compose from the temporary directory will ensure the workload
output appears on stdout for inspection:

```bash
$ cd [temporary directory]

# Start the compose project
$ docker-compose up

# Cleanup the compose project
$ docker-compose down --volumes
```
