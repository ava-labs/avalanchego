# Gecko

## Installation

Avalanche is an incredibly lightweight protocol, so the minimum computer requirements are quite modest.

- Hardware: 2 GHz or faster CPU, 3 GB RAM, 250 MB hard disk.
- OS: Ubuntu >= 18.04 or Mac OS X >= Catalina.
- Software: [Go](https://golang.org/doc/install) version >= 1.13.X and set up [`$GOPATH`](https://github.com/golang/go/wiki/SettingGOPATH).
- Network: IPv4 or IPv6 network connection, with an open public port.

### Native Install

Clone the Gecko repository:

```sh
go get -v -d github.com/ava-labs/gecko/...
cd $GOPATH/src/github.com/ava-labs/gecko
```

#### Building the Gecko Executable

Build Gecko using the build script:

```sh
./scripts/build.sh
```

The Gecko binary, named `avalanche`, is in the `build` directory.

### Docker Install

- Make sure you have docker installed on your machine (so commands like `docker run` etc. are available).
- Build the docker image of latest gecko branch by `scripts/build_image.sh`.
- Check the built image by `docker image ls`, you should see some image tagged
  `gecko-xxxxxxxx`, where `xxxxxxxx` is the commit id of the Gecko source it was built from.
- Test Gecko by `docker run -ti -p 9650:9650 -p 9651:9651 gecko-xxxxxxxx /gecko/build/avalanche
   --network-id=local --staking-enabled=false --snow-sample-size=1 --snow-quorum-size=1`. (For a production deployment,
  you may want to extend the docker image with required credentials for
  staking and TLS.)

## Running Gecko

### Connecting to Everest

To connect to the Everest Testnet, run:

```sh
./build/avalanche
```

You should see some pretty ASCII art and log messages.

You can use `Ctrl + C` to kill the node.

### Creating a Local Testnet

To create a single node testnet, run:

```sh
./build/avalanche --network-id=local --staking-enabled=false --snow-sample-size=1 --snow-quorum-size=1
```

This launches an Avalanche network with one node.
