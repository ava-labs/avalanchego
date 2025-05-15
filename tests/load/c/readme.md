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

There are more interesting metrics available from the tmpnet nodes being load tested. The following sub-sections explain how to set up the monitoring stack to visualize the metrics during the load tests.

## Prometheus

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

## Grafana

1. In a separate terminal, install and launch the Grafana service:

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
1. Create a dashboard at [localhost:3000/dashboard/new?editview=json-model](http://localhost:3000/dashboard/new?editview=json-model) and paste the JSON content of [`dashboard.json`](dashboard.json) into the text area, and click "Save changes".
1. Open the Load testing dashboard at [localhost:3000/d/aejze3k4d0mpsb/load-testing](http://localhost:3000/d/aejze3k4d0mpsb/load-testing)

## Run the load test

From the root of the repository:

```bash
./bin/ginkgo -v tests/load/c -- --avalanchego-path=$PWD/build/avalanchego
```
