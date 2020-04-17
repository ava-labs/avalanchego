# gecko

## Installation

AVA is an incredibly lightweight protocol, so the minimum computer requirements are quite modest.

- Hardware: 2 GHz or faster CPU, 3 GB RAM, 250 MB hard disk.
- OS: Ubuntu >= 18.04 or Mac OS X >= Catalina.
- Software: [Go](https://golang.org/doc/install) version >= 1.13.X and set up [`$GOPATH`](https://github.com/golang/go/wiki/SettingGOPATH).
- Network: IPv4 or IPv6 network connection, with an open public port.

### Native Install

Ubuntu users need the following libraries:

* libssl-dev
* libuv1-dev
* cmake
* make
* curl
* g++
  
Install the libraries:

```sh
sudo apt-get install libssl-dev libuv1-dev cmake make curl g++
```

#### Downloading Gecko Source Code

Clone the Gecko repository:

```sh
go get github.com/ava-labs/gecko
cd $GOPATH/src/github.com/ava-labs/gecko
```

#### Building the Gecko Executable

Build Gecko using the build script:

```sh
./scripts/build.sh
```

The Gecko binary, named `ava`, is in the `build` directory. 

### Docker Install

- Make sure you have docker installed on your machine (so commands like `docker run` etc. are available).
- Build the docker image of latest gecko branch by `scripts/build_image.sh`.
- Check the built image by `docker image ls`, you should see some image tagged
  `gecko-xxxxxxxx`, where `xxxxxxxx` is the commit id of the Gecko source it was built from.
- Test Gecko by `docker run -ti -p 9650:9650 -p 9651:9651 gecko-xxxxxxxx /gecko/build/ava
   --public-ip=127.0.0.1 --snow-sample-size=1 --snow-quorum-size=1 --staking-tls-enabled=false`. (For a production deployment,
  you may want to extend the docker image with required credentials for
  staking and TLS.)

## Running Gecko and Creating a Local Test Network

To create your own local test network, run:

```sh
./build/ava --public-ip=127.0.0.1 --snow-sample-size=1 --snow-quorum-size=1 --staking-tls-enabled=false
```

This launches an AVA network with one node.

You should see some pretty ASCII art and log messages.
You may see a few warnings. These are OK.

You can use `Ctrl + C` to kill the node.

If you want to specify your log level. You should set `--log-level` to one of the following values, in decreasing order of logging.
* `--log-level=verbo`
* `--log-level=debug`
* `--log-level=info`
* `--log-level=warn`
* `--log-level=error`
* `--log-level=fatal`
* `--log-level=off`
