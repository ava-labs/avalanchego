# Metrics

```mermaid
graph LR
    A[avalanche] --> B[chain]
    A --> C[network]
    A --> D[api]
    A --> E[db]
    A --> F[go]
    A --> G[health]
    A --> H[system_resources]
    A --> I[resource_tracker]
    A --> J[requests]
    B -- $chainID --> K[$vmID]
    B -- $chainID, $proposervmSide --> L[meterchainvm]
    B -- $chainID --> M[meterdagvm]
    B -- $chainID --> N[proposervm]
```
