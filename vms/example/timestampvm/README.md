# Timestamp Virtual Machine

[![Lint+Test+Build](https://github.com/ava-labs/timestampvm/actions/workflows/lint_test_build.yml/badge.svg)](https://github.com/ava-labs/timestampvm/actions/workflows/lint_test_build.yml)

Avalanche is a network composed of multiple blockchains. Each blockchain is an instance of a [Virtual Machine (VM)](https://docs.avax.network/learn/platform-overview#virtual-machines), much like an object in an object-oriented language is an instance of a class. That is, the VM defines the behavior of the blockchain.

TimestampVM defines a blockchain that is a timestamp server. Each block in the blockchain contains the timestamp when it was created along with a 32-byte piece of data (payload). Each block’s timestamp is after its parent’s timestamp. This VM demonstrates capabilities of custom VMs and custom blockchains. For more information, see: [Create a Virtual Machine](https://docs.avax.network/build/tutorials/platform/create-a-virtual-machine-vm)

## Running the VM
[`scripts/run.sh`](scripts/run.sh) automatically installs [avalanchego], sets up a local network,
and creates a `timestampvm` genesis file. To build and run E2E tests, you need to set the variable `E2E` before it: `E2E=true ./scripts/run.sh 1.7.11`

*Note: The above script relies on ginkgo to run successfully. Ensure that $GOPATH/bin is part of your $PATH before running the script.*  

_See [`tests/e2e`](tests/e2e) to see how it's set up and how its client requests are made._

```bash
# to startup a local cluster (good for development)
cd ${HOME}/go/src/github.com/ava-labs/timestampvm
./scripts/run.sh 1.9.3

# to run full e2e tests and shut down cluster afterwards
cd ${HOME}/go/src/github.com/ava-labs/timestampvm
E2E=true ./scripts/run.sh 1.9.3

# inspect cluster endpoints when ready
cat /tmp/avalanchego-v1.9.3/output.yaml
<<COMMENT
endpoint: /ext/bc/2VCAhX6vE3UnXC6s1CBPE6jJ4c4cHWMfPgCptuWS59pQ9vbeLM
logsDir: ...
pid: 12811
uris:
- http://127.0.0.1:9650
- http://127.0.0.1:9652
- http://127.0.0.1:9654
- http://127.0.0.1:9656
- http://127.0.0.1:9658
network-runner RPC server is running on PID 66810...

use the following command to terminate:

pkill -P 66810 && kill -2 66810 && pkill -9 -f tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH

# propose a block
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "timestampvm.proposeBlock",
    "params":{
        "data":"0x01020304000000000000000000000000000000000000000000000000000000003f004e9c"
    },
    "id": 1
}' -H 'content-type:application/json;' http://127.0.0.1:9652/ext/bc/2W3Gn3E3xKSeHQZP47iybpgH6pk3JRWbNQs9P2FrKvXcHSNteB
<<COMMENT
{"jsonrpc":"2.0","result":{"Success":true},"id":1}
COMMENT

# view last accepted block
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "timestampvm.getBlock",
    "params":{},
    "id": 1
}' -H 'content-type:application/json;' http://127.0.0.1:9652/ext/bc/2W3Gn3E3xKSeHQZP47iybpgH6pk3JRWbNQs9P2FrKvXcHSNteB
<<COMMENT
{"jsonrpc":"2.0","result":{"timestamp":"1668475950","data":"0x01020304000000000000000000000000000000000000000000000000000000003f004e9c","height":"1","id":"2RbyqtZcr8DWnxWjD2jLaPUsjd2cxMFbjz1kmJjR7gDpp3txvz","parentID":"SdVstz8FpkYxsneD2XQDk2CK7d1EBe4YVqkhftgbvUiyFfeHJ"},"id":1}
COMMENT

# terminate cluster
pkill -P 66810 && kill -2 66810 && pkill -9 -f tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH
```

## Load Testing the VM
Because `TimestampVM` is such a lightweight Virtual Machine, it is a great
candidate for testing the raw performance of the `ProposerVM` wrapper in
AvalancheGo.

To kickoff a load test, all you need to do is run the following command:
```bash
./scripts/tests.load.sh 1.9.3
```

This will automatically:
* disable all rate limiting rules in AvalancheGo
* activate the ProposerVM immediately (usually activates after 5 minutes on
  a new Subnet)
* set the ProposerVM block timer to have 0 delay (generate blocks as
  fast as possible)

When running, you'll see a set of logs printed out indicating the current
number of blocks per second **generated and finalized** on a local network:
```
INFO[11-18|09:19:58] Stats                                    height=0 avg bps=0.000 last bps=0.000
INFO[11-18|09:20:01] Stats                                    height=261 avg bps=86.795 last bps=87.000
INFO[11-18|09:20:04] Stats                                    height=597 avg bps=99.372 last bps=112.000
INFO[11-18|09:20:07] Stats                                    height=942 avg bps=104.566 last bps=115.000
INFO[11-18|09:20:10] Stats                                    height=1291 avg bps=107.493 last bps=116.333
INFO[11-18|09:20:13] Stats                                    height=1634 avg bps=108.854 last bps=114.333
INFO[11-18|09:20:16] Stats                                    height=1976 avg bps=109.664 last bps=114.000
INFO[11-18|09:20:19] Stats                                    height=2308 avg bps=109.804 last bps=110.667
INFO[11-18|09:20:22] Stats                                    height=2636 avg bps=109.742 last bps=109.333
INFO[11-18|09:20:25] Stats                                    height=2978 avg bps=110.213 last bps=114.000
INFO[11-18|09:20:28] Stats                                    height=3318 avg bps=110.512 last bps=113.333
INFO[11-18|09:20:31] Stats                                    height=3649 avg bps=110.489 last bps=110.333
INFO[11-18|09:20:34] Stats                                    height=3987 avg bps=110.667 last bps=112.667
INFO[11-18|09:20:37] Stats                                    height=4320 avg bps=110.691 last bps=111.000
INFO[11-18|09:20:40] Stats                                    height=4653 avg bps=110.711 last bps=111.000
INFO[11-18|09:20:43] Stats                                    height=4974 avg bps=110.453 last bps=107.000
INFO[11-18|09:20:46] Stats                                    height=5304 avg bps=110.423 last bps=110.000
INFO[11-18|09:20:49] Stats                                    height=5636 avg bps=110.434 last bps=110.667
INFO[11-18|09:20:52] Stats                                    height=5950 avg bps=110.101 last bps=104.667
INFO[11-18|09:20:55] Stats                                    height=6236 avg bps=109.317 last bps=95.333
INFO[11-18|09:20:58] Stats                                    height=6552 avg bps=109.116 last bps=105.333
INFO[11-18|09:21:01] Stats                                    height=6876 avg bps=109.061 last bps=108.000
INFO[11-18|09:21:04] Stats                                    height=7210 avg bps=109.163 last bps=111.333
INFO[11-18|09:21:07] Stats                                    height=7499 avg bps=108.574 last bps=96.333
INFO[11-18|09:21:10] Stats                                    height=7787 avg bps=108.049 last bps=96.000
INFO[11-18|09:21:13] Stats                                    height=8119 avg bps=108.152 last bps=110.667
INFO[11-18|09:21:16] Stats                                    height=8449 avg bps=108.222 last bps=110.000
INFO[11-18|09:21:19] Stats                                    height=8779 avg bps=108.279 last bps=110.000
INFO[11-18|09:21:22] Stats                                    height=9107 avg bps=108.315 last bps=109.333
INFO[11-18|09:21:25] Stats                                    height=9437 avg bps=108.372 last bps=110.000
INFO[11-18|09:21:28] Stats                                    height=9757 avg bps=108.315 last bps=106.667
INFO[11-18|09:21:31] Stats                                    height=10069 avg bps=108.175 last bps=104.000
INFO[11-18|09:21:34] Stats                                    height=10401 avg bps=108.242 last bps=110.667
```
