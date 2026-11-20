#!/usr/bin/env bash
set -euo pipefail

# Use real Go builds with an isolated cache, not simulated compiler output.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workdir="$(mktemp -d "${repo_root}/.build-cache-test.XXXXXX")"
trap 'rm -rf "${workdir}"' EXIT
export GOCACHE="${workdir}/cache" GOWORK=off
cd "${workdir}"
go mod init example.com/build-cache-test
mkdir dependency
printf 'package dependency\nfunc Value() int { return 1 }\n' > dependency/value.go
printf 'package main\nimport "example.com/build-cache-test/dependency"\nvar version string\nfunc main() { _ = dependency.Value(); _ = version }\n' > main.go
printf 'package main\nimport "testing"\nfunc TestMainPackage(t *testing.T) {}\n' > main_test.go

check() {
  "${repo_root}/scripts/workflow-validate-go-build-cache.sh" "${workdir}/dry-run.log"
}

# A cold cache must fail before any package compilation happens.
go build -n -o "${workdir}/cold" . > dry-run.log 2>&1
if check > "${workdir}/expected-failure.log" 2>&1; then
  echo 'cold cache unexpectedly passed validation' >&2
  exit 1
fi

# Ginkgo runs through go tool, which uses go run's compilation mode.
go mod edit -tool=example.com/build-cache-test
go tool example.com/build-cache-test
go run -n . > dry-run.log 2>&1
check

go build -o "${workdir}/producer" .
go build -n -o "${workdir}/consumer" . > dry-run.log 2>&1
check

# A different commit changes -ldflags even when all source files are unchanged.
modes=(no-race)
if [[ "$(go env CGO_ENABLED)" == 1 ]]; then
  modes+=(race)
fi
for mode in "${modes[@]}"; do
  flags=()
  if [[ "${mode}" == race ]]; then
    flags=(-race)
  fi
  go build "${flags[@]}" -ldflags '-X main.version=first' -o "${workdir}/first-version" .
  go build -n "${flags[@]}" -ldflags '-X main.version=second' -o "${workdir}/second-version" . > dry-run.log 2>&1
  # Ensure the regression case really requests main-package compilation.
  grep -E '/compile .* -p main ' dry-run.log > /dev/null
  check
  echo "Link-version regression passed (${mode})"
done

go build -n -gcflags=all=-N -o "${workdir}/changed-flags" . > dry-run.log 2>&1
if check > "${workdir}/expected-failure.log" 2>&1; then
  echo 'changed compiler flags unexpectedly passed validation' >&2
  exit 1
fi

# Test binaries need their own compiled packages, but must always execute later.
go test -c -o "${workdir}/producer.test" .
go test -n -c -o "${workdir}/consumer.test" . > dry-run.log 2>&1
check

# Main-package compilation is allowed, but dependency compilation is not.
printf '\nfunc changed() {}\n' >> main.go
go build -n -o "${workdir}/changed-main" . > dry-run.log 2>&1
grep -E '/compile .* -p main ' dry-run.log > /dev/null
check

printf '\nfunc changedDependency() {}\n' >> dependency/value.go
go build -n -o "${workdir}/changed-dependency" . > dry-run.log 2>&1
if check > "${workdir}/expected-failure.log" 2>&1; then
  echo 'changed dependency unexpectedly passed validation' >&2
  exit 1
fi
# Isolate the next check from the dependency change above.
go build -o "${workdir}/updated-producer" .

# Exercise CGO command detection and reuse with the real C compiler, if enabled.
if [[ "$(go env CGO_ENABLED)" == 1 ]]; then
  printf 'package dependency\n/* int value() { return 1; } */\nimport "C"\nfunc cValue() int { return int(C.value()) }\n' > dependency/cgo.go
  go build -n -o "${workdir}/cold-cgo" . > dry-run.log 2>&1
  if check > "${workdir}/expected-failure.log" 2>&1; then
    echo 'cold CGO cache unexpectedly passed validation' >&2
    exit 1
  fi
  go build -o "${workdir}/producer-cgo" .
  go build -n -o "${workdir}/consumer-cgo" . > dry-run.log 2>&1
  check
fi

: > dry-run.log
if check > "${workdir}/expected-failure.log" 2>&1; then
  echo 'empty log unexpectedly passed validation' >&2
  exit 1
fi
echo 'Go build-cache validation tests passed'
