The Health API can be used for measuring node health.

<Callout title="Note">
This API set is for a specific node; it is unavailable on the [public server](https://build.avax.network/docs/tooling/rpc-providers).
</Callout>

## Health Checks

The node periodically runs all health checks, including health checks for each chain.

The frequency at which health checks are run can be specified with the [\--health-check-frequency](https://build.avax.network/docs/nodes/configure/configs-flags) flag.

## Filterable Health Checks

The health checks that are run by the node are filterable. You can specify which health checks you want to see by using `tags` filters. Returned results will only include health checks that match the specified tags and global health checks like `network`, `database` etc. When filtered, the returned results will not show the full node health, but only a subset of filtered health checks. This means the node can still be unhealthy in unfiltered checks, even if the returned results show that the node is healthy. AvalancheGo supports using subnetIDs as tags.

## GET Request

To get an HTTP status code response that indicates the node's health, make a `GET` request. If the node is healthy, it will return a `200` status code. If the node is unhealthy, it will return a `503` status code. In-depth information about the node's health is included in the response body.

### Filtering

To filter GET health checks, add a `tag` query parameter to the request. The `tag` parameter is a string. For example, to filter health results by subnetID `29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL`, use the following query:

```sh
curl 'http://localhost:9650/ext/health?tag=29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL'
```

In this example returned results will contain global health checks and health checks that are related to subnetID `29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL`.

**Note**: This filtering can show healthy results even if the node is unhealthy in other Chains/Avalanche L1s.

In order to filter results by multiple tags, use multiple `tag` query parameters. For example, to filter health results by subnetID `29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL` and `28nrH5T2BMvNrWecFcV3mfccjs6axM1TVyqe79MCv2Mhs8kxiY` use the following query:

```sh
curl 'http://localhost:9650/ext/health?tag=29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL&tag=28nrH5T2BMvNrWecFcV3mfccjs6axM1TVyqe79MCv2Mhs8kxiY'
```

The returned results will include health checks for both subnetIDs as well as global health checks.

### Endpoints

The available endpoints for GET requests are:

- `/ext/health` returns a holistic report of the status of the node. **Most operators should monitor this status.**
- `/ext/health/health` is the same as `/ext/health`.
- `/ext/health/readiness` returns healthy once the node has finished initializing.
- `/ext/health/liveness` returns healthy once the endpoint is available.

## JSON RPC Request

### Format

This API uses the `json 2.0` RPC format. For more information on making JSON RPC calls, see [here](https://build.avax.network/docs/api-reference/guides/issuing-api-calls).

### Endpoint

### Methods

#### `health.health`

This method returns the last set of health check results.

**Example Call**:

```sh
curl  -H 'Content-Type: application/json' --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"health.health",
    "params": {
        "tags": ["11111111111111111111111111111111LpoYY", "29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL"]
    }
}' 'http://localhost:9650/ext/health'
```

**Example Response**:

```json
{
    "jsonrpc": "2.0",
    "result": {
        "checks": {
            "C": {
                "message": {
                    "engine": {
                        "consensus": {
                            "lastAcceptedHeight": 31273749,
                            "lastAcceptedID": "2Y4gZGzQnu8UjnHod8j1BLewHFVEbzhULPNzqrSWEHkHNqDrYL",
                            "longestProcessingBlock": "0s",
                            "processingBlocks": 0
                        },
                        "vm": null
                    },
                    "networking": {
                        "percentConnected": 0.9999592612587486
                    }
                },
                "timestamp": "2024-03-26T19:44:45.2931-04:00",
                "duration": 20375
            },
            "P": {
                "message": {
                    "engine": {
                        "consensus": {
                            "lastAcceptedHeight": 142517,
                            "lastAcceptedID": "2e1FEPCBEkG2Q7WgyZh1v4nt3DXj1HDbDthyhxdq2Ltg3shSYq",
                            "longestProcessingBlock": "0s",
                            "processingBlocks": 0
                        },
                        "vm": null
                    },
                    "networking": {
                        "percentConnected": 0.9999592612587486
                    }
                },
                "timestamp": "2024-03-26T19:44:45.293115-04:00",
                "duration": 8750
            },
            "X": {
                "message": {
                    "engine": {
                        "consensus": {
                            "lastAcceptedHeight": 24464,
                            "lastAcceptedID": "XuFCsGaSw9cn7Vuz5e2fip4KvP46Xu53S8uDRxaC2QJmyYc3w",
                            "longestProcessingBlock": "0s",
                            "processingBlocks": 0
                        },
                        "vm": null
                    },
                    "networking": {
                        "percentConnected": 0.9999592612587486
                    }
                },
                "timestamp": "2024-03-26T19:44:45.29312-04:00",
                "duration": 23291
            },
            "bootstrapped": {
                "message": [],
                "timestamp": "2024-03-26T19:44:45.293078-04:00",
                "duration": 3375
            },
            "database": {
                "timestamp": "2024-03-26T19:44:45.293102-04:00",
                "duration": 1959
            },
            "diskspace": {
                "message": {
                    "availableDiskBytes": 227332591616
                },
                "timestamp": "2024-03-26T19:44:45.293106-04:00",
                "duration": 3042
            },
            "network": {
                "message": {
                    "connectedPeers": 284,
                    "sendFailRate": 0,
                    "timeSinceLastMsgReceived": "293.098ms",
                    "timeSinceLastMsgSent": "293.098ms"
                },
                "timestamp": "2024-03-26T19:44:45.2931-04:00",
                "duration": 2333
            },
            "router": {
                "message": {
                    "longestRunningRequest": "66.90725ms",
                    "outstandingRequests": 3
                },
                "timestamp": "2024-03-26T19:44:45.293097-04:00",
                "duration": 3542
            }
        },
        "healthy": true
    },
    "id": 1
}
```

In this example response, every check has passed. So, the node is healthy.

**Response Explanation**:

- `checks` is a list of health check responses.
    - A check response may include a `message` with additional context.
    - A check response may include an `error` describing why the check failed.
    - `timestamp` is the timestamp of the last health check.
    - `duration` is the execution duration of the last health check, in nanoseconds.
    - `contiguousFailures` is the number of times in a row this check failed.
    - `timeOfFirstFailure` is the time this check first failed.
- `healthy` is true all the health checks are passing.

#### `health.readiness`

This method returns the last evaluation of the startup health check results.

**Example Call**:

```sh
curl  -H 'Content-Type: application/json' --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"health.readiness",
    "params": {
        "tags": ["11111111111111111111111111111111LpoYY", "29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL"]
    }
}' 'http://localhost:9650/ext/health'
```

**Example Response**:

```json
{
    "jsonrpc": "2.0",
    "result": {
        "checks": {
            "bootstrapped": {
                "message": [],
                "timestamp": "2024-03-26T20:02:45.299114-04:00",
                "duration": 2834
            }
        },
        "healthy": true
    },
    "id": 1
}
```

In this example response, every check has passed. So, the node has finished the startup process.

**Response Explanation**:

- `checks` is a list of health check responses.
    - A check response may include a `message` with additional context.
    - A check response may include an `error` describing why the check failed.
    - `timestamp` is the timestamp of the last health check.
    - `duration` is the execution duration of the last health check, in nanoseconds.
    - `contiguousFailures` is the number of times in a row this check failed.
    - `timeOfFirstFailure` is the time this check first failed.
- `healthy` is true all the health checks are passing.

#### `health.liveness`

This method returns healthy.

**Example Call**:

```sh
curl  -H 'Content-Type: application/json' --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"health.liveness"
}' 'http://localhost:9650/ext/health'
```

**Example Response**:

```json
{
    "jsonrpc": "2.0",
    "result": {
        "checks": {},
        "healthy": true
    },
    "id": 1
}
```

In this example response, the node was able to handle the request and mark the service as healthy.

**Response Explanation**:

- `checks` is an empty list.
- `healthy` is true.
