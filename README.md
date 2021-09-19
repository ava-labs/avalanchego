<div align="center">
  <img src="resources/AvalancheLogoRed.png?raw=true">
</div>

---

Node implementation for the [Avalanche](https://avax.network) network -
a blockchains platform with high throughput, and blazing fast transactions.

## Installation

Avalanche is an incredibly lightweight protocol, so the minimum computer requirements are quite modest.
Note that as network usage increases, hardware requirements may change.

- CPU: Equivalent of 8 AWS vCPU
- RAM: 16 GB
- Storage: 200 GB
- OS: Ubuntu 18.04/20.04 or MacOS >= Catalina
- Network: Reliable IPv4 or IPv6 network connection, with an open public port.
- Software Dependencies:
  - [Go](https://golang.org/doc/install) version >= 1.16.8 and set up [`$GOPATH`](https://github.com/golang/go/wiki/SettingGOPATH).
  - [gcc](https://gcc.gnu.org/)
  - g++

### Native Install

Clone the AvalancheGo repository:

```sh
go get -v -d github.com/ava-labs/avalanchego/...
cd $GOPATH/src/github.com/ava-labs/avalanchego
```

#### Building the Avalanche Executable

Build Avalanche using the build script:

```sh
./scripts/build.sh
```

The Avalanche binary, named `avalanchego`, is in the `build` directory.

### Binary Repository

Install AvalancheGo using an `apt` repository.

#### Adding the APT Repository

If you have already added the APT repository, you do not need to add it again.

To add the repository on Ubuntu 18.04 (Bionic), run:

```sh
sudo su -
wget -O - https://downloads.avax.network/avalanchego.gpg.key | apt-key add -
echo "deb https://downloads.avax.network/apt bionic main" > /etc/apt/sources.list.d/avalanche.list
exit
```

To add the repository on Ubuntu 20.04 (Focal), run:

```sh
sudo su -
wget -O - https://downloads.avax.network/avalanchego.gpg.key | apt-key add -
echo "deb https://downloads.avax.network/apt focal main" > /etc/apt/sources.list.d/avalanche.list
exit
```

#### Installing the Latest Version

After adding the APT repository, install avalanchego by running:

```sh
sudo apt update
sudo apt install avalanchego
```

### Binary Install

Download the [latest build](https://github.com/ava-labs/avalanchego/releases/latest) for your operating system and architecture.

The Avalanche binary to be executed is named `avalanchego`.

### Docker Install

Make sure docker is installed on the machine - so commands like `docker run` etc. are available.

Building the docker image of latest avalanchego branch can be done by running:

```sh
./scripts/build_image.sh
```

To check the built image, run:

```sh
docker image ls
```

The image should be tagged as `avaplatform/avalanchego:xxxxxxxx`, where `xxxxxxxx` is the shortened commit of the Avalanche source it was built from. To run the avalanche node, run:

```sh
docker run -ti -p 9650:9650 -p 9651:9651 avaplatform/avalanchego:xxxxxxxx /avalanchego/build/avalanchego
```

## Running Avalanche

### Connecting to Mainnet

To connect to the Avalanche Mainnet, run:

```sh
./build/avalanchego
```

You should see some pretty ASCII art and log messages.

You can use `Ctrl+C` to kill the node.

### Connecting to Fuji

To connect to the Fuji Testnet, run:

```sh
./build/avalanchego --network-id=fuji
```

### Creating a Local Testnet

To create a single node testnet, run:

```sh
./build/avalanchego --network-id=local --staking-enabled=false --snow-sample-size=1 --snow-quorum-size=1
```

This launches an Avalanche network with one node.

## Generating Code

Avalanchego uses multiple tools to generate efficient and boilerplate code.

### Running protobuf codegen

To regenerate the protobuf go code, run `scripts/protobuf_codegen.sh` from the root of the repo.

This should only be necessary when upgrading protobuf versions or modifying .proto definition files.

To use this script, you must have [protoc](https://grpc.io/docs/protoc-installation/) (v3.17.3), protoc-gen-go (v1.26.0) and protoc-gen-go-grpc (v1.1.0) installed. protoc must be on your $PATH.

To install the protoc dependencies:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.26
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.1
```

If you have not already, you may need to add `$GOPATH/bin` to your `$PATH`:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

If you extract protoc to ~/software/protobuf/, the following should work:

```sh
export PATH=$PATH:~/software/protobuf/bin/:~/go/bin
go get google.golang.org/protobuf/cmd/protoc-gen-go
go get google.golang.org/protobuf/cmd/protoc-gen-go-grpc
scripts/protobuf_codegen.sh
```

For more information, refer to the [GRPC Golang Quick Start Guide](https://grpc.io/docs/languages/go/quickstart/).

### Running protobuf codegen from docker

```sh
docker build -t avalanche:protobuf_codegen -f Dockerfile.protoc .
docker run -t -i -v $(pwd):/opt/avalanche -w/opt/avalanche avalanche:protobuf_codegen bash -c "scripts/protobuf_codegen.sh"
```

## Security Bugs

**We and our community welcome responsible disclosures.**

If you've discovered a security vulnerability, please report it via our [bug bounty program](https://hackenproof.com/avalanche/). Valid reports will be eligible for a reward (terms and conditions apply).
