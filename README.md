<div align="center">
  <img src="resources/camino-logo.png?raw=true">
</div>

---

Node implementation for the [Camino Network](https://camino.network) - a blockchain for the travel industry.

## Installation

Camino is an incredibly lightweight protocol, so the minimum computer requirements are quite modest.
Note that as network usage increases, hardware requirements may change.

The minimum recommended hardware specification for nodes connected to Mainnet is:

- CPU: Equivalent of 8 AWS vCPU
- RAM: 16 GiB
- Storage: 512 GiB
- OS: Ubuntu 20.04/22.04 or macOS >= 12
- Network: Reliable IPv4 or IPv6 network connection, with an open public port.

If you plan to build Camino-Node from source, you will also need the following software:

- [Go](https://golang.org/doc/install) version >= 1.19.6
- [gcc](https://gcc.gnu.org/)
- g++

### Official Documentation

Please refer to the official documentation available at [Camino Docs](https://docs.camino.network/camino-node) for the latest information.

### Native Install

Clone the caminogo repository:

```sh
git clone git@github.com:chain4travel/caminogo.git
cd caminogo
```

This will clone and checkout the `chain4travel` branch.

#### Building the Camino Node Executable

Build caminogo using the build script:

```sh
./scripts/build.sh
```

The Camino binary, named `caminogo`, is in the `build` directory.

### Binary Install

Download the [latest build](https://github.com/chain4travel/caminogo/releases/latest) for your operating system and architecture.

The Camino binary to be executed is named `caminogo`.

### Docker Install

Make sure docker is installed on the machine - so commands like `docker run` etc. are available.

Building the docker image of latest caminogo branch can be done by running:

```sh
./scripts/build_image.sh
```

To check the built image, run:

```sh
docker image ls
```

The image should be tagged as `c4tplatform/camino-node:xxxxxxxx`, where `xxxxxxxx` is the shortened commit of the Camino source it was built from. To run the Camino node, run:

```sh
docker run -ti -p 9650:9650 -p 9651:9651 c4tplatform/camino-node:xxxxxxxx /caminogo/build/caminogo
```

## Running Camino

### Connecting to Columbus Testnet

To connect to the Columbus Testnet, run:

```sh
./build/caminogo --network-id=columbus
```

You should see some pretty ASCII art and log messages.

You can use `Ctrl+C` to kill the node.

### Connecting to Camino Mainnet

To connect to the Mainnet, run:

```sh
./build/caminogo
```
For detailed instructions on running a validator node on the Camino Network, please refer to the [official validator guides](https://docs.camino.network/validator-guides).


### Creating a Local Testnet

See [this tutorial.](https://docs.camino.foundation/developer/build/create-a-local-test-network/)

## Bootstrapping

A node needs to catch up to the latest network state before it can participate in consensus and serve API calls.

A node will not [report healthy](https://docs.camino.foundation/developer/apis/camino-node-apis/health) until it is done bootstrapping.

## Generating Code

Camino-Node uses multiple tools to generate efficient and boilerplate code.

### Running protobuf codegen

To regenerate the protobuf go code, run `scripts/protobuf_codegen.sh` from the root of the repo.

This should only be necessary when upgrading protobuf versions or modifying .proto definition files.

To use this script, you must have [buf](https://docs.buf.build/installation) (v1.9.0), protoc-gen-go (v1.28.0) and protoc-gen-go-grpc (v1.2.0) installed.

To install the buf dependencies:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.0
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2.0
```

If you have not already, you may need to add `$GOPATH/bin` to your `$PATH`:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

If you extract buf to ~/software/buf/bin, the following should work:

```sh
export PATH=$PATH:~/software/buf/bin/:~/go/bin
go get google.golang.org/protobuf/cmd/protoc-gen-go
go get google.golang.org/protobuf/cmd/protoc-gen-go-grpc
scripts/protobuf_codegen.sh
```

For more information, refer to the [GRPC Golang Quick Start Guide](https://grpc.io/docs/languages/go/quickstart/).

### Running protobuf codegen from docker

```sh
docker build -t chain4travel:protobuf_codegen -f api/Dockerfile.buf .
docker run -t -i -v $(pwd):/opt/chain4travel -w/opt/chain4travel chain4travel:protobuf_codegen bash -c "scripts/protobuf_codegen.sh"
```

### Running mock codegen

To regenerate the [gomock](https://github.com/golang/mock) code, run `scripts/mock.gen.sh` from the root of the repo.

This should only be necessary when modifying exported interfaces or after modifying `scripts/mock.mockgen.txt`.

## Versioning

### Version Semantics

CaminoGo is first and foremost a client for the Camino network. The versioning of CaminoGo follows that of the Camino network.

- `v0.x.x` indicates a development network version.
- `v1.x.x` indicates a production network version.
- `vx.[Upgrade].x` indicates the number of network upgrades that have occurred.
- `vx.x.[Patch]` indicates the number of client upgrades that have occurred since the last network upgrade.

### Library Compatibility Guarantees

Because CaminoGo's version denotes the network version, it is expected that interfaces exported by CaminoGo's packages may change in `Patch` version updates.

### API Compatibility Guarantees

APIs exposed when running CaminoGo will maintain backwards compatibility, unless the functionality is explicitly deprecated and announced when removed.

## Supported Platforms

Camino-Node can run on different platforms, with different support tiers:

- **Tier 1**: Fully supported by the maintainers, guaranteed to pass all tests including e2e and stress tests.
- **Tier 2**: Passes all unit and integration tests but not necessarily e2e tests.
- **Tier 3**: Builds but lightly tested (or not), considered _experimental_.
- **Not supported**: May not build and not tested, considered _unsafe_. To be supported in the future.

The following table lists currently supported platforms and their corresponding
Camino-Node support tiers:

| Architecture | Operating system | Support tier  |
| :----------: | :--------------: | :-----------: |
|    amd64     |      Linux       |       1       |
|    arm64     |      Linux       |       2       |
|    amd64     |      Darwin      |       2       |
|    amd64     |     Windows      |       3       |
|     arm      |      Linux       | Not supported |
|     i386     |      Linux       | Not supported |
|    arm64     |      Darwin      | Not supported |

To officially support a new platform, one must satisfy the following requirements:

| Camino-Node continuous integration    | Tier 1  | Tier 2  | Tier 3  |
| ---------------------------------- | :-----: | :-----: | :-----: |
| Build passes                       | &check; | &check; | &check; |
| Unit and integration tests pass    | &check; | &check; |         |
| End-to-end and stress tests pass   | &check; |         |         |

## Security Bugs

**We take security issues seriously and encourage responsible disclosures from our community.**

If you have discovered a security vulnerability, please refer to our [Security Policy](SECURITY.md) or contact us on [Discord](https://discord.gg/camino).


**We and our community welcome responsible disclosures.**

If you've discovered a security vulnerability, please report it to us via [discord](https://discord.gg/K5THjAweFB). Valid reports will be eligible for a reward (terms and conditions apply).
