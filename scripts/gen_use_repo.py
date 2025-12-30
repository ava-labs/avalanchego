#!/usr/bin/env python3
"""Generate use_repo list from go.mod for MODULE.bazel."""

import re
import sys

def go_module_to_repo_name(module_path):
    """Convert a Go module path to a Bazel repository name."""
    # Remove version suffix like /v2, /v3
    module_path = re.sub(r'/v\d+$', '', module_path)

    # Common prefixes
    replacements = [
        (r'^connectrpc\.com/', 'com_connectrpc_'),
        (r'^github\.com/', 'com_github_'),
        (r'^golang\.org/x/', 'org_golang_x_'),
        (r'^google\.golang\.org/', 'org_golang_google_'),
        (r'^gopkg\.in/', 'in_gopkg_'),
        (r'^k8s\.io/', 'io_k8s_'),
        (r'^go\.uber\.org/', 'org_uber_go_'),
        (r'^gonum\.org/v1/', 'org_gonum_v1_'),
        (r'^go\.opentelemetry\.io/', 'io_opentelemetry_go_'),
        (r'^sigs\.k8s\.io/', 'io_k8s_sigs_'),
    ]

    result = module_path
    for pattern, replacement in replacements:
        result = re.sub(pattern, replacement, result)

    # Replace special characters with underscores
    result = re.sub(r'[/.\-]', '_', result)

    return result

def parse_go_mod(filename):
    """Parse go.mod and extract all dependencies."""
    deps = []
    in_require = False

    with open(filename) as f:
        for line in f:
            line = line.strip()

            # Track require blocks
            if line.startswith('require ('):
                in_require = True
                continue
            if line == ')':
                in_require = False
                continue

            # Single-line require
            if line.startswith('require ') and '(' not in line:
                parts = line.split()
                if len(parts) >= 2:
                    deps.append(parts[1])
                continue

            # Inside require block
            if in_require and line and not line.startswith('//'):
                parts = line.split()
                if parts:
                    deps.append(parts[0])

    return deps

def main():
    go_mod = sys.argv[1] if len(sys.argv) > 1 else 'go.mod'

    deps = parse_go_mod(go_mod)

    # Filter out local replacements (graft modules)
    deps = [d for d in deps if 'ava-labs/avalanchego/graft' not in d]

    # Convert to repo names and dedupe
    repo_names = sorted(set(go_module_to_repo_name(d) for d in deps))

    print('use_repo(')
    print('    go_deps,')
    for name in repo_names:
        print(f'    "{name}",')
    print(')')

if __name__ == '__main__':
    main()
