#!/usr/bin/env bash

set -euo pipefail


# Ensure required tools are installed
if ! command -v solc &> /dev/null; then
  echo "Error: solc installation failed. Run this command within Nix shell."
  exit 1
fi

CONTRACTS_DIR="$(dirname "$0")"

for FILE in "${CONTRACTS_DIR}"/*.sol; do
  echo "Generating Go bindings from Solidity contract $FILE..."
  CONTRACT_NAME=$(basename "$FILE" .sol)
  solc --abi --bin --overwrite -o "$CONTRACTS_DIR" "${CONTRACTS_DIR}/${CONTRACT_NAME}.sol"
  go run github.com/ava-labs/libevm/cmd/abigen@latest \
    --bin="${CONTRACTS_DIR}/${CONTRACT_NAME}.bin" \
    --abi="${CONTRACTS_DIR}/${CONTRACT_NAME}.abi" \
    --type "${CONTRACT_NAME}" \
    --pkg=contracts \
    --out="${CONTRACTS_DIR}/${CONTRACT_NAME}.bindings.go"
  rm "${CONTRACTS_DIR}/${CONTRACT_NAME}.bin" "${CONTRACTS_DIR}/${CONTRACT_NAME}.abi"
  echo "Generated ${CONTRACT_NAME}.bindings.go"
done


{
  "annotations": {
    "list": [
      {
        "builtIn": 1,
        "datasource": {
          "type": "datasource",
          "uid": "grafana"
        },
        "enable": true,
        "hide": true,
        "iconColor": "rgba(0, 211, 255, 1)",
        "name": "Annotations & Alerts",
        "type": "dashboard"
      }
    ]
  },
  "description": "v0 iteration of performance",
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 0,
  "id": 10,
  "links": [
    {
      "asDropdown": false,
      "icon": "external link",
      "includeVars": true,
      "keepTime": true,
      "tags": [
        "Avalanche"
      ],
      "targetBlank": false,
      "title": "",
      "tooltip": "",
      "type": "dashboards",
      "url": ""
    }
  ],
  "panels": [
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "Shows transaction flow from submission to acceptance",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              }
            ]
          }
        },
        "overrides": [
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "A"
            },
            "properties": [
              {
                "id": "displayName",
                "value": "Issued TX"
              }
            ]
          },
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "B"
            },
            "properties": [
              {
                "id": "displayName",
                "value": "TXs Built"
              }
            ]
          },
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "C"
            },
            "properties": [
              {
                "id": "displayName",
                "value": "TXs Verified"
              }
            ]
          },
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "D"
            },
            "properties": [
              {
                "id": "displayName",
                "value": "TXs Accepted"
              }
            ]
          }
        ]
      },
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 0,
        "y": 0
      },
      "id": 16,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "12.0.0",
      "targets": [
        {
          "editorMode": "code",
          "expr": "sum(rate(load_txs_issued[5m]))",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "sum(rate(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_txs_built[5m]))",
          "hide": false,
          "instant": false,
          "legendFormat": "__auto",
          "range": true,
          "refId": "B"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "sum(rate(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_txs_verified[5m]))",
          "hide": false,
          "instant": false,
          "legendFormat": "__auto",
          "range": true,
          "refId": "C"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "sum(rate(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_txs_accepted[5m]))",
          "hide": false,
          "instant": false,
          "legendFormat": "__auto",
          "range": true,
          "refId": "D"
        }
      ],
      "title": "Transaction Lifecycle",
      "type": "timeseries"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "Breakdown of time spent in each processing stage",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              },
              {
                "color": "red",
                "value": 80
              }
            ]
          },
          "unit": "ns"
        },
        "overrides": [
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "A"
            },
            "properties": [
              {
                "id": "displayName",
                "value": "Root Calculation"
              }
            ]
          },
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "B"
            },
            "properties": [
              {
                "id": "displayName",
                "value": "State Operation"
              }
            ]
          },
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "C"
            },
            "properties": [
              {
                "id": "displayName",
                "value": "State OP per TX"
              }
            ]
          }
        ]
      },
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 12,
        "y": 0
      },
      "id": 17,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "12.0.0",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "sum(rate(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_root_calculated_sum[1m]) / rate(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_root_calculated_count[1m]))",
          "hide": false,
          "instant": false,
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "exemplar": false,
          "expr": "sum(rate(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_state_operations[1m]))",
          "hide": false,
          "instant": false,
          "legendFormat": "__auto",
          "range": true,
          "refId": "B"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "sum(rate(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_wait_root_sum[1m]) / rate(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_wait_root_count[1m]))",
          "hide": false,
          "instant": false,
          "legendFormat": "Wait Root",
          "range": true,
          "refId": "C"
        }
      ],
      "title": "Processing Stage Breakdown",
      "type": "timeseries"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "Percentage of executor tasks that run concurrently without being blocked indicating execution parallelism efficiency\n\nThe query measures the ratio of executable tasks to all tasks (executable + blocked) during block building, which directly indicates how efficiently your blockchain can parallelize transaction execution during the building phase. Higher values (closer to 100%) indicate better parallelism with fewer dependencies blocking execution.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              }
            ]
          },
          "unit": "percent"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 0,
        "y": 8
      },
      "id": 18,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "12.0.0",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "increase(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_executor_build_executable[5m]) / (increase(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_executor_build_executable[5m]) + increase(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_executor_build_blocked[5m])) * 100",
          "hide": false,
          "instant": false,
          "legendFormat": "{{node_id}} - Build",
          "range": true,
          "refId": "A"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "increase(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_executor_verify_executable[5m]) / (increase(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_executor_verify_executable[5m]) + increase(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_chain_chain_executor_verify_blocked[5m])) * 100",
          "hide": false,
          "instant": false,
          "legendFormat": "{{node_id}} - Verify",
          "range": true,
          "refId": "B"
        }
      ],
      "title": "Transaction Parallelism Efficiency",
      "type": "timeseries"
    },
    {
      "collapsed": false,
      "gridPos": {
        "h": 1,
        "w": 24,
        "x": 0,
        "y": 16
      },
      "id": 21,
      "panels": [],
      "title": "Resource",
      "type": "row"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "This ratio between total CPU usage and processing time gives  insight into overall system efficiency. Higher values suggest CPU bottlenecks affecting the entire system.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              }
            ]
          },
          "unit": "Cores / Req"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 0,
        "y": 17
      },
      "id": 19,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "12.0.0",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "sum(avalanche_resource_tracker_cpu_usage) / sum(avalanche_resource_tracker_processing_time > 0)",
          "hide": false,
          "instant": false,
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "CPU vs Processing Time",
      "type": "timeseries"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "Resource saturation metrics that indicate when the system is reaching capacity limits",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              }
            ]
          }
        },
        "overrides": [
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "A"
            },
            "properties": [
              {
                "id": "unit",
                "value": "percent"
              },
              {
                "id": "displayName",
                "value": "CPU"
              }
            ]
          },
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "B"
            },
            "properties": [
              {
                "id": "unit",
                "value": "binBps"
              },
              {
                "id": "displayName",
                "value": "Alloc. Bytes"
              }
            ]
          }
        ]
      },
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 12,
        "y": 17
      },
      "id": 20,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "12.0.0",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "sum(100 * avalanche_resource_tracker_processing_time / on() group_left count(avalanche_resource_tracker_cpu_usage))",
          "hide": false,
          "instant": false,
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "sum(rate(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_process_go_memstats_alloc_bytes_total[1m]))",
          "hide": false,
          "instant": false,
          "legendFormat": "__auto",
          "range": true,
          "refId": "B"
        }
      ],
      "title": "Resource Pressure Indicators",
      "type": "timeseries"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              }
            ]
          }
        },
        "overrides": [
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "B"
            },
            "properties": [
              {
                "id": "unit",
                "value": "bytes"
              }
            ]
          }
        ]
      },
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 0,
        "y": 25
      },
      "id": 22,
      "options": {
        "minVizHeight": 75,
        "minVizWidth": 75,
        "orientation": "auto",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showThresholdLabels": false,
        "showThresholdMarkers": true,
        "sizing": "auto"
      },
      "pluginVersion": "11.5.0",
      "targets": [
        {
          "editorMode": "code",
          "expr": "avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_process_go_goroutines",
          "legendFormat": "Num. goroutines",
          "range": true,
          "refId": "A"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_process_go_memstats_heap_inuse_bytes{chain=~\"$chainID\", node_id=~\"$nodeID\", network_uuid=~\"$networkID\"}",
          "hide": false,
          "instant": false,
          "legendFormat": "Heap in use",
          "range": true,
          "refId": "B"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_process_go_threads{chain=~\"$chainID\", node_id=~\"$nodeID\", network_uuid=~\"$networkID\"}",
          "hide": false,
          "instant": false,
          "legendFormat": "System threads ",
          "range": true,
          "refId": "C"
        }
      ],
      "title": "Go Gauge Metrics",
      "type": "gauge"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "Shows the distribution of transaction confirmation times across percentiles",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              },
              {
                "color": "red",
                "value": 80
              }
            ]
          },
          "unit": "ms"
        },
        "overrides": [
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "A"
            },
            "properties": [
              {
                "id": "displayName",
                "value": "50 Percentile"
              }
            ]
          },
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "B"
            },
            "properties": [
              {
                "id": "displayName",
                "value": "75 percentile"
              }
            ]
          },
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "C"
            },
            "properties": [
              {
                "id": "displayName",
                "value": "95 percentile"
              }
            ]
          },
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "D"
            },
            "properties": [
              {
                "id": "displayName",
                "value": "99 percentile"
              }
            ]
          }
        ]
      },
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 12,
        "y": 25
      },
      "id": 13,
      "options": {
        "minVizHeight": 75,
        "minVizWidth": 75,
        "orientation": "auto",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showThresholdLabels": false,
        "showThresholdMarkers": true,
        "sizing": "auto"
      },
      "pluginVersion": "11.5.0",
      "targets": [
        {
          "editorMode": "code",
          "expr": "histogram_quantile(0.5, sum(rate(load_tx_latency_bucket[1m])) by (le))",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "histogram_quantile(0.75, sum(rate(load_tx_latency_bucket[1m])) by (le))",
          "hide": false,
          "instant": false,
          "legendFormat": "__auto",
          "range": true,
          "refId": "B"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "histogram_quantile(0.95, sum(rate(load_tx_latency_bucket[1m])) by (le))",
          "hide": false,
          "instant": false,
          "legendFormat": "__auto",
          "range": true,
          "refId": "C"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "histogram_quantile(0.99, sum(rate(load_tx_latency_bucket[1m])) by (le))",
          "hide": false,
          "instant": false,
          "legendFormat": "__auto",
          "range": true,
          "refId": "D"
        }
      ],
      "title": "Latency Distribution Metrics",
      "type": "gauge"
    },
    {
      "collapsed": false,
      "gridPos": {
        "h": 1,
        "w": 24,
        "x": 0,
        "y": 33
      },
      "id": 15,
      "panels": [],
      "title": "Latency",
      "type": "row"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "How transaction latency changes as the system load increases",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              }
            ]
          }
        },
        "overrides": [
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "A"
            },
            "properties": [
              {
                "id": "custom.axisPlacement",
                "value": "right"
              },
              {
                "id": "unit",
                "value": "ms"
              }
            ]
          },
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "B"
            },
            "properties": [
              {
                "id": "custom.axisPlacement",
                "value": "left"
              },
              {
                "id": "unit",
                "value": "reqps"
              }
            ]
          },
          {
            "matcher": {
              "id": "byFrameRefID",
              "options": "C"
            },
            "properties": [
              {
                "id": "unit",
                "value": "reqps"
              }
            ]
          }
        ]
      },
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 0,
        "y": 34
      },
      "id": 12,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "11.5.0",
      "targets": [
        {
          "editorMode": "code",
          "expr": "sum(rate(load_tx_latency_sum[1m]) / rate(load_tx_latency_count[1m]))",
          "legendFormat": "Latency",
          "range": true,
          "refId": "A"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "sum(rate(load_txs_issued[1m]))",
          "hide": false,
          "instant": false,
          "legendFormat": "Issued TXs",
          "range": true,
          "refId": "B"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "editorMode": "code",
          "expr": "sum(rate(avalanche_qCNyZHrs3rZX458wPJXPJJypPf6w423A84jnfbdP2TPEmEE9u_vm_hypervm_vm_txs_submitted[1m]))",
          "hide": false,
          "instant": false,
          "legendFormat": "Received TXs",
          "range": true,
          "refId": "C"
        }
      ],
      "title": "Latency Under Load",
      "type": "timeseries"
    },
    {
      "collapsed": false,
      "gridPos": {
        "h": 1,
        "w": 24,
        "x": 0,
        "y": 42
      },
      "id": 1,
      "panels": [],
      "title": "MeterchainVM",
      "type": "row"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "Shows the average time taken to parse blocks by the MeterChainVM.\n\nSpikes may indicate VM-level slowness, resource pressure, or unusual transaction loads.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              },
              {
                "color": "red",
                "value": 80
              }
            ]
          },
          "unit": "ns"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 11,
        "w": 8,
        "x": 0,
        "y": 43
      },
      "id": 3,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "11.5.0",
      "targets": [
        {
          "editorMode": "code",
          "expr": "sum(rate(avalanche_meterchainvm_parse_block_sum[5m])) / sum(rate(avalanche_meterchainvm_parse_block_count[5m]))",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "Average Parse Block Time",
      "type": "timeseries"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "Shows the average time taken to build blocks by the MeterChainVM.\n\nSpikes may indicate VM-level slowness, resource pressure, or unusual transaction loads.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              },
              {
                "color": "red",
                "value": 80
              }
            ]
          },
          "unit": "ns"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 11,
        "w": 8,
        "x": 8,
        "y": 43
      },
      "id": 4,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "11.5.0",
      "targets": [
        {
          "editorMode": "code",
          "expr": "sum(rate(avalanche_meterchainvm_build_block_sum[5m])) / sum(rate(avalanche_meterchainvm_build_block_count[5m]))",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "Average Build Block Time",
      "type": "timeseries"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "Shows the average time taken to verify blocks by the MeterChainVM.\n\nSpikes may indicate VM-level slowness, resource pressure, or unusual transaction loads.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              },
              {
                "color": "red",
                "value": 80
              }
            ]
          },
          "unit": "ns"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 11,
        "w": 8,
        "x": 16,
        "y": 43
      },
      "id": 2,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "11.5.0",
      "targets": [
        {
          "editorMode": "code",
          "expr": "sum(rate(avalanche_meterchainvm_verify_sum[5m])) / sum(rate(avalanche_meterchainvm_verify_count[5m]))",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "Average Verify Block Time",
      "type": "timeseries"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "Shows the average time taken to accept blocks by the MeterChainVM.\n\nSpikes may indicate VM-level slowness, resource pressure, or unusual transaction loads.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              },
              {
                "color": "red",
                "value": 80
              }
            ]
          },
          "unit": "ns"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 11,
        "w": 8,
        "x": 0,
        "y": 54
      },
      "id": 5,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "11.5.0",
      "targets": [
        {
          "editorMode": "code",
          "expr": "sum(rate(avalanche_meterchainvm_accept_sum[5m])) / sum(rate(avalanche_meterchainvm_accept_count[5m]))",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "Average Accept Block Time",
      "type": "timeseries"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "Shows the average time taken to reject blocks by the MeterChainVM.\n\nSpikes may indicate VM-level slowness, resource pressure, or unusual transaction loads.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              },
              {
                "color": "red",
                "value": 80
              }
            ]
          },
          "unit": "ns"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 11,
        "w": 8,
        "x": 8,
        "y": 54
      },
      "id": 6,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "11.5.0",
      "targets": [
        {
          "editorMode": "code",
          "expr": "sum(rate(avalanche_meterchainvm_reject_sum[5m])) / sum(rate(avalanche_meterchainvm_reject_count[5m]))",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "Average Reject Block Time",
      "type": "timeseries"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "description": "Shows the average time taken to get a blocks by the MeterChainVM.\n\nSpikes may indicate VM-level slowness, resource pressure, or unusual transaction loads.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 0,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "auto",
            "spanNulls": false,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green"
              },
              {
                "color": "red",
                "value": 80
              }
            ]
          },
          "unit": "ns"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 11,
        "w": 8,
        "x": 16,
        "y": 54
      },
      "id": 7,
      "options": {
        "legend": {
          "calcs": [],
          "displayMode": "list",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "single",
          "sort": "none"
        }
      },
      "pluginVersion": "11.5.0",
      "targets": [
        {
          "editorMode": "code",
          "expr": "sum(rate(avalanche_meterchainvm_get_block_sum[5m])) / sum(rate(avalanche_meterchainvm_get_block_count[5m]))",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "Average Get Block Time",
      "type": "timeseries"
    }
  ],
  "preload": false,
  "refresh": "5s",
  "schemaVersion": 41,
  "tags": [
    "Performance",
    "Avalanche"
  ],
  "templating": {
    "list": [
      {
        "current": {
          "text": "prometheus",
          "value": "P1809F7CD0C75ACF3"
        },
        "label": "Data Source",
        "name": "datasource",
        "options": [],
        "query": "prometheus",
        "refresh": 1,
        "regex": "",
        "type": "datasource"
      },
      {
        "allowCustomValue": true,
        "baseFilters": [],
        "datasource": {
          "type": "prometheus",
          "uid": "${datasource}"
        },
        "filters": [
          {
            "condition": "",
            "key": "network_uuid",
            "keyLabel": "network_uuid",
            "operator": "=",
            "value": "3789f33e-be9d-4a1c-9d95-121394841acd",
            "valueLabels": [
              "3789f33e-be9d-4a1c-9d95-121394841acd"
            ]
          }
        ],
        "label": "Filter",
        "name": "filter",
        "type": "adhoc"
      }
    ]
  },
  "time": {
    "from": "now-6h",
    "to": "now"
  },
  "timepicker": {},
  "timezone": "America/Los_Angeles",
  "title": "MorpheusVM Performance",
  "uid": "cejpdf8fl064ge",
  "version": 21
}
