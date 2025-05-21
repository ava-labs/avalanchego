# Load testing

The C-chain load test entrypoint is in [ginkgo_test.go](ginkgo_test.go).

It runs with 5 nodes and 5 "agents".

Each "agent" runs a transaction issuer and a transaction listener asynchronously,
and is assigned uniformly to the nodes available, via websocket connections.

There are two load tests:

1. "Simple" load test, where transactions issued are zero-fund transfers to the sender address.
2. "Complex" load test, where [this contract](contracts/EVMLoadSimulator.sol) is deployed and transactions call functions of this contract at random with random parameters. This contract has different functions, each testing a particular performance aspect of the EVM, for example memory writes.

From the load test perspective, only the TPS (transactions per second) is logged out. Metrics available are:

- total transactions issued `txs_issued`
- total transactions confirmed `txs_confirmed`
- total transactions failed `txs_failed`
- transaction latency histogram `tx_latency`

There are more interesting metrics available from the tmpnet nodes being load tested.

If you have the Grafana credentials, the easiest way to visualize the metrics is to click the Grafana URL logged by the load test, in the form

```log
INFO metrics and logs available via grafana (collectors must be running)     {"url": "https://grafana-poc.avax-dev.network/d/kBQpRdWnk/avalanche-main-dashboard?&var-filter=network_uuid%7C%3D%7Cdb0cb247-e0a6-46e1-b265-4ac6b3c8f6c4&var-filter=is_ephemeral_node%7C%3D%7Cfalse&from=1747816981769&to=now"}
```

If you don't, follow the local steps in the [visualize metrics locally](#visualize-metrics-locally) section.

Finally, to run the load test, from the root of the repository:

```bash
./bin/ginkgo -v tests/load/c -- --avalanchego-path=$PWD/build/avalanchego
```

## Visualize metrics locally

1. Navigate to this directory with `cd tests/load/c`.
1. Setup the Prometheus configuration file: `envsubst < prometheus.template.yml > prometheus.yml`
1. Launch Prometheus using the dev shell:

    ```bash
    nix develop
    ```

    ```nix
    prometheus --config.file prometheus.yml
    ```

    This starts Prometheus listening on port `9090`.
1. In a separate terminal, install and launch the Grafana service. For example on MacOS:

    ```bash
    brew install grafana
    brew services start grafana
    ```

1. Open Grafana in your browser at [localhost:3000](http://localhost:3000) and log in with the default credentials `admin` and `admin`.
1. Add a new Prometheus data source starting at [localhost:3000/connections/datasources/prometheus](http://localhost:3000/connections/datasources/prometheus)
    1. Click on "Add new data source"
    1. Name it `prometheus`
    1. In the Connection section, set the URL to `http://localhost:9090`
    1. Click the "Save & Test" button at the bottom to verify the connection.
1. Create a dashboard at [localhost:3000/dashboard/new?editview=json-model](http://localhost:3000/dashboard/new?editview=json-model) and paste the JSON content of [`dashboard.json`](https://github.com/ava-labs/avalanche-monitoring/blob/main/grafana/dashboards/c_chain_load.json) into the text area, and click "Save changes".
1. Open the Load testing dashboard at [localhost:3000/d/aejze3k4d0mpsb/load-testing](http://localhost:3000/d/aejze3k4d0mpsb/load-testing)
