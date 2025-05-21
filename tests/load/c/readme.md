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

Finally, to run the load test, run:

```bash
./bin/ginkgo -v tests/load/c -- --avalanchego-path=$PWD/build/avalanchego
```

## Visualize metrics in Grafana

### Private remote instances

If you have the credentials (internal to Ava Labs) for the remote Prometheus and Grafana PoC, you can visualize the metrics following these steps:

1. Start the dev shell to have Prometheus setup to scrape the load test metrics and send it to the remote Prometheus instance:

    ```bash
    nix develop
    ```

1. Set your Prometheus credentials using the credentials you can find in your password manager

    ```bash
    export PROMETHEUS_USERNAME=<username>
    export PROMETHEUS_PASSWORD=<password>
    export LOKI_USERNAME=<username>
    export LOKI_PASSWORD=<password>
    ```

1. Run the load test:

    ```bash
    ./bin/ginkgo -v tests/load/c -- --avalanchego-path=$PWD/build/avalanchego --start-collectors
    ```

1. Wait for the load test to finish, this will log out a URL at the end of the test, in the form

    ```log
    INFO metrics and logs available via grafana (collectors must be running)     {"uri": "https://grafana-poc.avax-dev.network/d/eabddd1d-0a06-4ba1-8e68-a44504e37535/C-Chain%20Load?from=1747817500582&to=1747817952631&var-filter=network_uuid%7C%3D%7C4f419e3a-dba5-4ccd-b2fd-bda15f9826ff"}
    ```

1. Open the URL in your browser, and log in with the Grafana credentials which you can find in your password manager.

For reference, see [the tmpnet monitoring section](../../fixture/tmpnet/README.md#monitoring)

### Locally

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
