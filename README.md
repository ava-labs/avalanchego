<div align="center">
  <img src="resources/camino-logo.png?raw=true">
</div>

---

SDK for the [Camino](https://chain4travel.com) network -
a blockchains platform for the touristic market.

## Introduction

Beginning with v0.2.0 CaminoGo does not any longer build binaries.
Instead it is used as the core SDK for other components in the Camino environment
like [camino-node](https://github.com/chain4travel/camino-node), [caminoethvm](https://github.com/chain4travel/caminoethvm), [camino-network-runner](https://github.com/chain4travel/camino-network-runner) ....

Reason is, that there have been circular dependencies between the different go
modules which made it hard to deploy binaries based on the same caminogo implementation

## Generating Code

Caminogo uses multiple tools to generate efficient and boilerplate code.

### Running protobuf codegen

To regenerate the protobuf go code, run `scripts/protobuf_codegen.sh` from the root of the repo.

This should only be necessary when upgrading protobuf versions or modifying .proto definition files.

To use this script, you must have [buf](https://docs.buf.build/installation) (v1.0.0-rc12), protoc-gen-go (v1.27.1) and protoc-gen-go-grpc (v1.2.0) installed.

To install the buf dependencies:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.27.1
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

## Supported Platforms

CaminoGo can be used on different platforms, with different support tiers:

- **Tier 1**: Fully supported by the maintainers, guaranteed to pass all tests including e2e and stress tests.
- **Tier 2**: Passes all unit and integration tests but not necessarily e2e tests.
- **Tier 3**: Builds but lightly tested (or not), considered _experimental_.
- **Not supported**: May not build and not tested, considered _unsafe_. To be supported in the future.

The following table lists currently supported platforms and their corresponding
CaminoGo support tiers:

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

| CaminoGo continuous integration    | Tier 1  | Tier 2  | Tier 3  |
| ---------------------------------- | :-----: | :-----: | :-----: |
| Build passes                       | &check; | &check; | &check; |
| Unit and integration tests pass    | &check; | &check; |         |
| End-to-end and stress tests pass   | &check; |         |         |

## Security Bugs

**We and our community welcome responsible disclosures.**

If you've discovered a security vulnerability, please report it to us via [discord](https://discord.gg/K5THjAweFB). Valid reports will be eligible for a reward (terms and conditions apply).
