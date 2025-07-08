# Load testing

The C-chain load test entrypoint is in [main.go](main/main.go).

It runs with 5 nodes and 5 "agents".

Each "agent" runs a transaction issuer and a transaction listener asynchronously,
and is assigned uniformly to the nodes available, via websocket connections.

The load test picks at weighted random a transaction type to generate and issue, defined in [issuer.go](issuer.go).

For some transaction types, [this contract](contracts/EVMLoadSimulator.sol) is deployed and transactions call functions of this contract. This contract has different functions, each testing a particular performance aspect of the EVM, for example memory writes.

From the load test perspective, only the TPS (transactions per second) is logged out. Metrics available are:

- total transactions issued `txs_issued`
- total transactions confirmed `txs_confirmed`
- total transactions failed `txs_failed`
- transaction latency histogram `tx_latency`

There are more interesting metrics available from the tmpnet nodes being load tested.

# How to run

Initiate nix shell: `nix develop`

## Local tmpnet

To run the load test locally against tmpnet nodes:

```bash
./scripts/run_task.sh install-nix
```

### Against local kind cluster

Builds image from local code and deploys to local Kind cluster:

```shell
./scripts/run_task.sh test-load-kube-kind
```

### With additional CLI flags (duration, connections, nodes, etc.)
```shell
./scripts/run_task.sh test-load-kube-kind -- --duration=10m --nodes=3
```

### With custom image (skips building from local code)
```shell
./scripts/run_task.sh test-load-kube-kind -- --kube-image=my-registry/avalanchego:v1.2.3
```

## CI job on self-hosted runner

Use GitHub Actions workflow "Load Tests on k8s":
- Go to Actions tab in GitHub
- Select "Load Tests on k8s" workflow
- Click "Run workflow"
- Specify image (defaults to avaplatform/avalanchego:latest)
- Additional parameters can be passed via workflow inputs (duration, connections, node count)

## Visualize metrics in Grafana

### Private remote instances

If you have the credentials (internal to Ava Labs) for the CI monitoring stack:

1. Set your monitoring credentials using the credentials you can find in your password manager:

    ```bash
    export PROMETHEUS_USERNAME=<username>
    export PROMETHEUS_PASSWORD=<password>
    export LOKI_USERNAME=<username>
    export LOKI_PASSWORD=<password>
    ```
2. Run the load test

3. Prior to the test beginning, you will see the following log:

    ```log
    INFO metrics and logs available via grafana (collectors must be running)     {"uri": "https://grafana-poc.avax-dev.network/d/eabddd1d-0a06-4ba1-8e68-a44504e37535/C-Chain%20Load?from=1747817500582&to=1747817952631&var-filter=network_uuid%7C%3D%7C4f419e3a-dba5-4ccd-b2fd-bda15f9826ff"}
    ```

4. Open the URL in your browser, and log in with the Grafana credentials which you can find in your password manager.

For reference on setting up local monitoring, see [the tmpnet monitoring section](../../fixture/tmpnet/README.md#monitoring)
