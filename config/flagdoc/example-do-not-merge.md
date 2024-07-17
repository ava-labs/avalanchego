## AvalancheGo

This is shown under the title
and
can
be
multiline

### Synopsis

This is shown under the "Synopsis" header
and
can
be
multiline

### Options

```
      --acp-object ints                                                 ACPs to object adoption
      --acp-support ints                                                ACPs to support adoption
      --add-primary-network-delegator-fee uint                          Transaction fee, in nAVAX, for transactions that add new primary network delegators
      --add-primary-network-validator-fee uint                          Transaction fee, in nAVAX, for transactions that add new primary network validators
      --add-subnet-delegator-fee uint                                   Transaction fee, in nAVAX, for transactions that add new subnet delegators (default 1000000)
      --add-subnet-validator-fee uint                                   Transaction fee, in nAVAX, for transactions that add new subnet validators (default 1000000)
      --api-admin-enabled                                               If true, this node exposes the Admin API
      --api-health-enabled                                              If true, this node exposes the Health API (default true)
      --api-info-enabled                                                If true, this node exposes the Info API (default true)
      --api-keystore-enabled                                            If true, this node exposes the Keystore API
      --api-metrics-enabled                                             If true, this node exposes the Metrics API (default true)
      --benchlist-duration duration                                     Max amount of time a peer is benchlisted after surpassing the threshold (default 15m0s)
      --benchlist-fail-threshold int                                    Number of consecutive failed queries before benchlisting a node (default 10)
      --benchlist-min-failing-duration duration                         Minimum amount of time messages to a peer must be failing before the peer is benched (default 2m30s)
      --bootstrap-ancestors-max-containers-received uint                This node reads at most this many containers from an incoming Ancestors message (default 2000)
      --bootstrap-ancestors-max-containers-sent uint                    Max number of containers in an Ancestors message sent by this node (default 2000)
      --bootstrap-beacon-connection-timeout duration                    Timeout before emitting a warn log when connecting to bootstrapping beacons (default 1m0s)
      --bootstrap-ids string                                            Comma separated list of bootstrap peer ids to connect to. Example: NodeID-JR4dVmy6ffUGAKCBDkyCbeZbyHQBeDsET,NodeID-8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z
      --bootstrap-ips string                                            Comma separated list of bootstrap peer ips to connect to. Example: 127.0.0.1:9630,127.0.0.1:9631
      --bootstrap-max-time-get-ancestors duration                       Max Time to spend fetching a container and its ancestors when responding to a GetAncestors (default 50ms)
      --chain-aliases-file string                                       Specifies a JSON file that maps blockchainIDs with custom aliases. Ignored if chain-config-content is specified (default "$AVALANCHEGO_DATA_DIR/configs/chains/aliases.json")
      --chain-aliases-file-content string                               Specifies base64 encoded map from blockchainID to custom aliases
      --chain-config-content string                                     Specifies base64 encoded chains configurations
      --chain-config-dir string                                         Chain specific configurations parent directory. Ignored if chain-config-content is specified (default "$AVALANCHEGO_DATA_DIR/configs/chains")
      --chain-data-dir string                                           Chain specific data directory (default "$AVALANCHEGO_DATA_DIR/chainData")
      --config-file string                                              Specifies a config file. Ignored if config-file-content is specified
      --config-file-content string                                      Specifies base64 encoded config content
      --config-file-content-type string                                 Specifies the format of the base64 encoded config content. Available values: 'json', 'yaml', 'toml' (default "json")
      --consensus-app-concurrency uint                                  Maximum number of goroutines to use when handling App messages on a chain (default 2)
      --consensus-frontier-poll-frequency duration                      Frequency of polling for new consensus frontiers (default 100ms)
      --consensus-shutdown-timeout duration                             Timeout before killing an unresponsive chain (default 1m0s)
      --create-asset-tx-fee uint                                        Transaction fee, in nAVAX, for transactions that create new assets (default 1000000)
      --create-blockchain-tx-fee uint                                   Transaction fee, in nAVAX, for transactions that create new blockchains (default 100000000)
      --create-subnet-tx-fee uint                                       Transaction fee, in nAVAX, for transactions that create new subnets (default 100000000)
      --data-dir string                                                 Sets the base data directory where default sub-directories will be placed unless otherwise specified. (default "$HOME/.avalanchego")
      --db-config-file string                                           Path to database config file. Ignored if db-config-file-content is specified
      --db-config-file-content string                                   Specifies base64 encoded database config content
      --db-dir string                                                   Path to database directory (default "$AVALANCHEGO_DATA_DIR/db")
      --db-read-only                                                    If true, database writes are to memory and never persisted. May still initialize database directory/files on disk if they don't exist
      --db-type string                                                  Database type to use. Must be one of {leveldb, memdb, pebbledb} (default "leveldb")
      --fd-limit uint                                                   Attempts to raise the process file descriptor limit to at least this value and error if the value is above the system max (default 10240)
      --genesis-file string                                             Specifies a genesis config file path. Ignored when running standard networks or if genesis-file-content is specified
      --genesis-file-content string                                     Specifies base64 encoded genesis content
      --health-check-averager-halflife duration                         Halflife of averager when calculating a running average in a health check (default 10s)
      --health-check-frequency duration                                 Time between health checks (default 30s)
  -h, --help                                                            help for AvalancheGo
      --http-allowed-hosts strings                                      List of acceptable host names in API requests. Provide the wildcard ('*') to accept requests from all hosts. API requests where the Host field is empty or an IP address will always be accepted. An API call whose HTTP Host field isn't acceptable will receive a 403 error code (default [localhost])
      --http-allowed-origins string                                     Origins to allow on the HTTP port. Defaults to * which allows all origins. Example: https://*.avax.network https://*.avax-test.network (default "*")
      --http-host string                                                Address of the HTTP server. If the address is empty or a literal unspecified IP address, the server will bind on all available unicast and anycast IP addresses of the local system (default "127.0.0.1")
      --http-idle-timeout duration                                      Maximum duration to wait for the next request when keep-alives are enabled. If http-idle-timeout is zero, the value of http-read-timeout is used. If both are zero, there is no timeout. (default 2m0s)
      --http-port uint                                                  Port of the HTTP server. If the port is 0 a port number is automatically chosen (default 9650)
      --http-read-header-timeout duration                               Maximum duration to read request headers. The connection's read deadline is reset after reading the headers. If http-read-header-timeout is zero, the value of http-read-timeout is used. If both are zero, there is no timeout. (default 30s)
      --http-read-timeout duration                                      Maximum duration for reading the entire request, including the body. A zero or negative value means there will be no timeout (default 30s)
      --http-shutdown-timeout duration                                  Maximum duration to wait for existing connections to complete during node shutdown (default 10s)
      --http-shutdown-wait duration                                     Duration to wait after receiving SIGTERM or SIGINT before initiating shutdown. The /health endpoint will return unhealthy during this duration
      --http-tls-cert-file string                                       TLS certificate file for the HTTPs server. Ignored if http-tls-cert-file-content is specified
      --http-tls-cert-file-content string                               Specifies base64 encoded TLS certificate for the HTTPs server
      --http-tls-enabled                                                Upgrade the HTTP server to HTTPs
      --http-tls-key-file string                                        TLS private key file for the HTTPs server. Ignored if http-tls-key-file-content is specified
      --http-tls-key-file-content string                                Specifies base64 encoded TLS private key for the HTTPs server
      --http-write-timeout duration                                     Maximum duration before timing out writes of the response. It is reset whenever a new request's header is read. A zero or negative value means there will be no timeout. (default 30s)
      --index-allow-incomplete                                          If true, allow running the node in such a way that could cause an index to miss transactions. Ignored if index is disabled
      --index-enabled                                                   If true, index all accepted containers and transactions and expose them via an API
      --log-dir string                                                  Logging directory for Avalanche (default "$AVALANCHEGO_DATA_DIR/logs")
      --log-disable-display-plugin-logs                                 Disables displaying plugin logs in stdout.
      --log-display-level string                                        The log display level. If left blank, will inherit the value of log-level. Otherwise, should be one of {verbo, debug, trace, info, warn, error, fatal, off}
      --log-format string                                               The structure of log format. Defaults to 'auto' which formats terminal-like logs, when the output is a terminal. Otherwise, should be one of {auto, plain, colors, json} (default "auto")
      --log-level string                                                The log level. Should be one of {verbo, debug, trace, info, warn, error, fatal, off} (default "info")
      --log-rotater-compress-enabled                                    Enables the compression of rotated log files through gzip.
      --log-rotater-max-age uint                                        The maximum number of days to retain old log files based on the timestamp encoded in their filename. 0 means retain all old log files.
      --log-rotater-max-files uint                                      The maximum number of old log files to retain. 0 means retain all old log files. (default 7)
      --log-rotater-max-size uint                                       The maximum file size in megabytes of the log file before it gets rotated. (default 8)
      --max-stake-duration duration                                     Maximum staking duration (default 8760h0m0s)
      --max-validator-stake uint                                        Maximum stake, in nAVAX, that can be placed on a validator on the primary network (default 3000000000000000)
      --meter-vms-enabled                                               Enable Meter VMs to track VM performance with more granularity (default true)
      --min-delegation-fee uint                                         Minimum delegation fee, in the range [0, 1000000], that can be charged for delegation on the primary network (default 20000)
      --min-delegator-stake uint                                        Minimum stake, in nAVAX, that can be delegated on the primary network (default 25000000000)
      --min-stake-duration duration                                     Minimum staking duration (default 24h0m0s)
      --min-validator-stake uint                                        Minimum stake, in nAVAX, required to validate the primary network (default 2000000000000)
      --network-allow-private-ips                                       Allows the node to initiate outbound connection attempts to peers with private IPs. If the provided --network-id is one of [mainnet, fuji] the default is false. Oterhwise, the default is true
      --network-compression-type string                                 Compression type for outbound messages. Must be one of [zstd, none] (default "zstd")
      --network-health-max-outstanding-request-duration duration        Node reports unhealthy if there has been a request outstanding for this duration (default 5m0s)
      --network-health-max-portion-send-queue-full float                Network layer returns unhealthy if more than this portion of the pending send queue is full (default 0.9)
      --network-health-max-send-fail-rate float                         Network layer reports unhealthy if more than this portion of attempted message sends fail (default 0.9)
      --network-health-max-time-since-msg-received duration             Network layer returns unhealthy if haven't received a message for at least this much time (default 1m0s)
      --network-health-max-time-since-msg-sent duration                 Network layer returns unhealthy if haven't sent a message for at least this much time (default 1m0s)
      --network-health-min-conn-peers uint                              Network layer returns unhealthy if connected to less than this many peers (default 1)
      --network-id string                                               Network ID this node will connect to (default "mainnet")
      --network-inbound-connection-throttling-cooldown duration         Upgrade an inbound connection from a given IP at most once per this duration. If 0, don't rate-limit inbound connection upgrades (default 10s)
      --network-inbound-connection-throttling-max-conns-per-sec float   Max number of inbound connections to accept (from all peers) per second (default 256)
      --network-initial-reconnect-delay duration                        Initial delay duration must be waited before attempting to reconnect a peer (default 1s)
      --network-initial-timeout duration                                Initial timeout value of the adaptive timeout manager (default 5s)
      --network-max-clock-difference duration                           Max allowed clock difference value between this node and peers (default 1m0s)
      --network-max-reconnect-delay duration                            Maximum delay duration must be waited before attempting to reconnect a peer (default 1m0s)
      --network-maximum-inbound-timeout duration                        Maximum timeout value of an inbound message. Defines duration within which an incoming message must be fulfilled. Incoming messages containing deadline higher than this value will be overridden with this value. (default 10s)
      --network-maximum-timeout duration                                Maximum timeout value of the adaptive timeout manager (default 10s)
      --network-minimum-timeout duration                                Minimum timeout value of the adaptive timeout manager (default 2s)
      --network-outbound-connection-throttling-rps uint                 Make at most this number of outgoing peer connection attempts per second (default 50)
      --network-outbound-connection-timeout duration                    Timeout when dialing a peer (default 30s)
      --network-peer-list-bloom-reset-frequency duration                Frequency to recalculate the bloom filter used to request new peers from other nodes (default 1m0s)
      --network-peer-list-num-validator-ips uint                        Number of validator IPs to gossip to other nodes (default 15)
      --network-peer-list-pull-gossip-frequency duration                Frequency to request peers from other nodes (default 2s)
      --network-peer-read-buffer-size uint                              Size, in bytes, of the buffer that we read peer messages into (there is one buffer per peer) (default 8192)
      --network-peer-write-buffer-size uint                             Size, in bytes, of the buffer that we write peer messages into (there is one buffer per peer) (default 8192)
      --network-ping-frequency duration                                 Frequency of pinging other peers (default 22.5s)
      --network-ping-timeout duration                                   Timeout value for Ping-Pong with a peer (default 30s)
      --network-read-handshake-timeout duration                         Timeout value for reading handshake messages (default 15s)
      --network-require-validator-to-connect                            If true, this node will only maintain a connection with another node if this node is a validator, the other node is a validator, or the other node is a beacon
      --network-tcp-proxy-enabled                                       Require all P2P connections to be initiated with a TCP proxy header
      --network-tcp-proxy-read-timeout duration                         Maximum duration to wait for a TCP proxy header (default 3s)
      --network-timeout-coefficient float                               Multiplied by average network response time to get the network timeout. Must be >= 1 (default 2)
      --network-timeout-halflife duration                               Halflife of average network response time. Higher value --> network timeout is less volatile. Can't be 0 (default 5m0s)
      --network-tls-key-log-file-unsafe string                          TLS key log file path. Should only be specified for debugging
      --partial-sync-primary-network                                    Only sync the P-chain on the Primary Network. If the node is a Primary Network validator, it will report unhealthy
      --plugin-dir string                                               Path to the plugin directory (default "$AVALANCHEGO_DATA_DIR/plugins")
      --process-context-file string                                     The path to write process context to (including PID, API URI, and staking address). (default "$AVALANCHEGO_DATA_DIR/process.json")
      --profile-continuous-enabled                                      Whether the app should continuously produce performance profiles
      --profile-continuous-freq duration                                How frequently to rotate performance profiles (default 15m0s)
      --profile-continuous-max-files int                                Maximum number of historical profiles to keep (default 5)
      --profile-dir string                                              Path to the profile directory (default "$AVALANCHEGO_DATA_DIR/profiles")
      --proposervm-use-current-height                                   Have the ProposerVM always report the last accepted P-chain block height
      --public-ip string                                                Public IP of this node for P2P communication
      --public-ip-resolution-frequency duration                         Frequency at which this node resolves/updates its public IP and renew NAT mappings, if applicable (default 5m0s)
      --public-ip-resolution-service string                             Only acceptable values are "opendns", "ifconfigco" or "ifconfigme". When provided, the node will use that service to periodically resolve/update its public IP
      --router-health-max-drop-rate float                               Node reports unhealthy if the router drops more than this portion of messages (default 1)
      --router-health-max-outstanding-requests uint                     Node reports unhealthy if there are more than this many outstanding consensus requests (Get, PullQuery, etc.) over all chains (default 1024)
      --snow-commit-threshold int                                       Beta value to use for consensus (default 20)
      --snow-concurrent-repolls int                                     Minimum number of concurrent polls for finalizing consensus (default 4)
      --snow-confidence-quorum-size int                                 Threshold of nodes required to increase this node's confidence in a network poll. Ignored if snow-quorum-size is provided (default 15)
      --snow-max-processing int                                         Maximum number of processing items to be considered healthy (default 256)
      --snow-max-time-processing duration                               Maximum amount of time an item should be processing and still be healthy (default 30s)
      --snow-optimal-processing int                                     Optimal number of processing containers in consensus (default 10)
      --snow-preference-quorum-size int                                 Threshold of nodes required to update this node's preference in a network poll. Ignored if snow-quorum-size is provided (default 15)
      --snow-quorum-size int                                            Threshold of nodes required to update this node's preference and increase its confidence in a network poll (default 15)
      --snow-sample-size int                                            Number of nodes to query for each network poll (default 20)
      --stake-max-consumption-rate uint                                 Maximum consumption rate of the remaining tokens to mint in the staking function (default 120000)
      --stake-min-consumption-rate uint                                 Minimum consumption rate of the remaining tokens to mint in the staking function (default 100000)
      --stake-minting-period duration                                   Consumption period of the staking function (default 8760h0m0s)
      --stake-supply-cap uint                                           Supply cap of the staking function (default 720000000000000000)
      --staking-ephemeral-cert-enabled                                  If true, the node uses an ephemeral staking TLS key and certificate, and has an ephemeral node ID
      --staking-ephemeral-signer-enabled                                If true, the node uses an ephemeral staking signer key
      --staking-host string                                             Address of the consensus server. If the address is empty or a literal unspecified IP address, the server will bind on all available unicast and anycast IP addresses of the local system
      --staking-port uint                                               Port of the consensus server. If the port is 0 a port number is automatically chosen (default 9651)
      --staking-signer-key-file string                                  Path to the signer private key for staking. Ignored if staking-signer-key-file-content is specified (default "$AVALANCHEGO_DATA_DIR/staking/signer.key")
      --staking-signer-key-file-content string                          Specifies base64 encoded signer private key for staking
      --staking-tls-cert-file string                                    Path to the TLS certificate for staking. Ignored if staking-tls-cert-file-content is specified (default "$AVALANCHEGO_DATA_DIR/staking/staker.crt")
      --staking-tls-cert-file-content string                            Specifies base64 encoded TLS certificate for staking
      --staking-tls-key-file string                                     Path to the TLS private key for staking. Ignored if staking-tls-key-file-content is specified (default "$AVALANCHEGO_DATA_DIR/staking/staker.key")
      --staking-tls-key-file-content string                             Specifies base64 encoded TLS private key for staking
      --state-sync-ids string                                           Comma separated list of state sync peer ids to connect to. Example: NodeID-JR4dVmy6ffUGAKCBDkyCbeZbyHQBeDsET,NodeID-8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z
      --state-sync-ips string                                           Comma separated list of state sync peer ips to connect to. Example: 127.0.0.1:9630,127.0.0.1:9631
      --subnet-config-content string                                    Specifies base64 encoded subnets configurations
      --subnet-config-dir string                                        Subnet specific configurations parent directory. Ignored if subnet-config-content is specified (default "$AVALANCHEGO_DATA_DIR/configs/subnets")
      --sybil-protection-disabled-weight uint                           Weight to provide to each peer when sybil protection is disabled (default 100)
      --sybil-protection-enabled                                        Enables sybil protection. If enabled, Network TLS is required (default true)
      --system-tracker-cpu-halflife duration                            Halflife to use for the cpu tracker. Larger halflife --> cpu usage metrics change more slowly (default 15s)
      --system-tracker-disk-halflife duration                           Halflife to use for the disk tracker. Larger halflife --> disk usage metrics change more slowly (default 1m0s)
      --system-tracker-disk-required-available-space uint               Minimum number of available bytes on disk, under which the node will shutdown. (default 536870912)
      --system-tracker-disk-warning-threshold-available-space uint      Warning threshold for the number of available bytes on disk, under which the node will be considered unhealthy.  Must be >= [system-tracker-disk-required-available-space] (default 1073741824)
      --system-tracker-frequency duration                               Frequency to check the real system usage of tracked processes. More frequent checks --> usage metrics are more accurate, but more expensive to track (default 500ms)
      --system-tracker-processing-halflife duration                     Halflife to use for the processing requests tracker. Larger halflife --> usage metrics change more slowly (default 15s)
      --throttler-inbound-at-large-alloc-size uint                      Size, in bytes, of at-large byte allocation in inbound message throttler (default 6291456)
      --throttler-inbound-bandwidth-max-burst-size uint                 Max inbound bandwidth a node can use at once. Must be at least the max message size. See BandwidthThrottler (default 2097152)
      --throttler-inbound-bandwidth-refill-rate uint                    Max average inbound bandwidth usage of a peer, in bytes per second. See BandwidthThrottler (default 524288)
      --throttler-inbound-cpu-max-non-validator-node-usage float        Maximum number of CPUs that a non-validator can utilize. Value should be in range [0, total core count] (default 2)
      --throttler-inbound-cpu-max-non-validator-usage float             Number of CPUs that if fully utilized, will rate limit all non-validators. Value should be in range [0, total core count] (default 12.8)
      --throttler-inbound-cpu-max-recheck-delay duration                In the CPU-based network throttler, check at least this often whether the node's CPU usage has fallen to an acceptable level (default 5s)
      --throttler-inbound-cpu-validator-alloc float                     Maximum number of CPUs to allocate for use by validators. Value should be in range [0, total core count] (default 16)
      --throttler-inbound-disk-max-non-validator-node-usage float       Maximum number of disk reads/writes per second that a non-validator can utilize. Must be >= 0 (default 1.073741824e+12)
      --throttler-inbound-disk-max-non-validator-usage float            Number of disk reads/writes per second that, if fully utilized, will rate limit all non-validators. Must be >= 0 (default 1.073741824e+12)
      --throttler-inbound-disk-max-recheck-delay duration               In the disk-based network throttler, check at least this often whether the node's disk usage has fallen to an acceptable level (default 5s)
      --throttler-inbound-disk-validator-alloc float                    Maximum number of disk reads/writes per second to allocate for use by validators. Must be > 0 (default 1.073741824e+12)
      --throttler-inbound-node-max-at-large-bytes uint                  Max number of bytes a node can take from the inbound message throttler's at-large allocation. Must be at least the max message size (default 2097152)
      --throttler-inbound-node-max-processing-msgs uint                 Max number of messages currently processing from a given node (default 1024)
      --throttler-inbound-validator-alloc-size uint                     Size, in bytes, of validator byte allocation in inbound message throttler (default 33554432)
      --throttler-outbound-at-large-alloc-size uint                     Size, in bytes, of at-large byte allocation in outbound message throttler (default 33554432)
      --throttler-outbound-node-max-at-large-bytes uint                 Max number of bytes a node can take from the outbound message throttler's at-large allocation. Must be at least the max message size (default 2097152)
      --throttler-outbound-validator-alloc-size uint                    Size, in bytes, of validator byte allocation in outbound message throttler (default 33554432)
      --tracing-enabled                                                 If true, enable opentelemetry tracing
      --tracing-endpoint string                                         The endpoint to send trace data to (default "localhost:4317")
      --tracing-exporter-type string                                    Type of exporter to use for tracing. Options are [grpc, http] (default "grpc")
      --tracing-headers stringToString                                  The headers to provide the trace indexer (default [])
      --tracing-insecure                                                If true, don't use TLS when sending trace data (default true)
      --tracing-sample-rate float                                       The fraction of traces to sample. If >= 1, always sample. If <= 0, never sample (default 0.1)
      --track-subnets string                                            List of subnets for the node to track. A node tracking a subnet will track the uptimes of the subnet validators and attempt to sync all the chains in the subnet. Before validating a subnet, a node should be tracking the subnet to avoid impacting their subnet validation uptime
      --transform-subnet-tx-fee uint                                    Transaction fee, in nAVAX, for transactions that transform subnets (default 100000000)
      --tx-fee uint                                                     Transaction fee, in nAVAX (default 1000000)
      --uptime-metric-freq duration                                     Frequency of renewing this node's average uptime metric (default 30s)
      --uptime-requirement float                                        Fraction of time a validator must be online to receive rewards (default 0.8)
      --version                                                         If true, print version and quit
      --version-json                                                    If true, print version in JSON format and quit
      --vm-aliases-file string                                          Specifies a JSON file that maps vmIDs with custom aliases. Ignored if vm-aliases-file-content is specified (default "$AVALANCHEGO_DATA_DIR/configs/vms/aliases.json")
      --vm-aliases-file-content string                                  Specifies base64 encoded maps vmIDs with custom aliases
```

###### Auto generated by spf13/cobra on 17-Jul-2024
