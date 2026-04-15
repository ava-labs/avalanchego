# Extracts the optimizer settings from a solc combined.json.
.contracts[].metadata | fromjson | .settings.optimizer