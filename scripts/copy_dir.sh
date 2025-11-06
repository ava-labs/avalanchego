#!/usr/bin/env bash

set -euo pipefail

# Usage: ./scripts/copy_dir.sh source_directory destination_directory
# Assumes s5cmd has been installed and is available in the PATH.
# s5cmd is included in the nix dev shell.

if [ $# -ne 2 ]; then
    echo "Usage: $0 <source_directory> <destination_directory>"
    echo "Import from S3 URL Example: $0 's3://bucket1/path1' /dest/dir"
    echo "Import from S3 object key Example: $0 'cchain-mainnet-blocks-1m-ldb' /dest/dir"
    echo "Export to S3 Example: $0 '/local/path1' 's3://bucket2/path2'"
    echo "Local Example: $0 '/local/path1' /dest/dir"
    exit 1
fi

SRC="$1"
DST="$2"

# If SRC doesn't start with s3:// or /, assume it's an S3 object key
if [[ "$SRC" != s3://* ]] && [[ "$SRC" != /* ]]; then
    echo "Error: SRC must be either an S3 URL (s3://...), a local path (/...), or already expanded"
    echo "If using an object key, expand it before calling this script"
    exit 1
fi

# Function to copy from a single source to destination
function copy_source() {
    local source="$1"
    local dest="$2"

    # Check if source starts with s3://
    if [[ "$source" == s3://* || "$dest" == s3://* ]]; then
        # Use s5cmd to copy from S3
        echo "Copying from S3: $source to $dest"
        time s5cmd cp --show-progress "$source" "$dest"
    else
        # Use cp for local filesystem with recursive support

        # Ensure destination directory exists
        mkdir -p "$dest"

        if [ -d "$source" ]; then
            time cp -r "$source"/* "$dest/"
        elif [ -f "$source" ]; then
            time cp "$source" "$dest/"
        else
            echo "Warning: Source not found: $source"
            return 1
        fi
    fi
}

# Function to check the destination directory does not exist to avoid
# overwrites
function check_dst_not_exists() {
    local dst="$1"

    if [[ "$dst" == s3://* ]]; then
        # Validate the S3 path format as s3://<bucket-name>/<directory-name>/
        echo "Checking S3 path format: $dst"
        if ! [[ "$dst" =~ ^s3://[^/]+/([^/]+/)$ ]]; then
          echo "Error: Invalid S3 path format."
          echo "Expected format: s3://<bucket-name>/<directory-name>/"
          exit 1
        fi

        # Note: S3 tooling does not provide a native way to check for an empty
        # directory. To avoid accidental overwrites, we use a best-effort, brittle
        # workaround that relies on the expected status code and error message
        # from s5cmd ls.
        # If the error message changes, this script would be expected to fail
        # by misreporting non-existent directories as existing, which means
        # a change in behavior would cause the script to fail to copy rather
        # than allow accidental overwrites.
        echo "Checking if S3 path exists: $dst"
        if ! OUTPUT=$(s5cmd ls "$dst" 2>&1); then
            # If the command fails, check for the expected error message.
            if [[ "$OUTPUT" == *"no object found"* ]]; then
                echo "Verified S3 destination: '$dst' is empty"
            else
                echo "Error: failed to check for contents of $dst"
                echo "$OUTPUT"
                exit 1
            fi
        else
            # Success indicates a non-empty destination, so we exit with an error.
            echo "Cannot copy to non-empty destination: '$dst':"
            echo "$OUTPUT"
            exit 1
        fi
    else
        echo "Checking if local path exists: $dst"
        if [[ -e "$dst" ]]; then
            echo "Local destination directory '$dst' already exists. Exiting."
            exit 1
        fi
    fi
}

check_dst_not_exists "$DST"

copy_source "$SRC" "$DST"
