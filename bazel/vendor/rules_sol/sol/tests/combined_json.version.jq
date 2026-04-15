# Extracts the version from a solc combined.json, stripping the platform to
# leave only the semver and commit; e.g. 0.7.6+commit.7338295f
.version | match("(\\d\\.\\d\\.\\d\\+commit\\.[0-9a-f]+)").captures[0].string | tojson