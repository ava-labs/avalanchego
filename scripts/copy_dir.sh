#!/usr/bin/env bash

set -euo pipefail

# Usage: ./scripts/copy_dir.sh source_directory destination_directory
# Sources can be S3 URLs (s3://bucket/path) or a local file path
# Assumes s5cmd has been installed and is available in the PATH.
# s5cmd is included in the nix dev shell.

if [ $# -ne 2 ]; then
    echo "Usage: $0 <source_directory> <destination_directory>"
    echo "Import from S3 Example: $0 's3://bucket1/path1' /dest/dir"
    echo "Import zip from S3 Example: $0 's3://bucket1/path1.zip' /dest/dir"
    echo "Export to S3 Example: $0 '/local/path1' 's3://bucket2/path2'"
    echo "Local Example: $0 '/local/path1' /dest/dir"
    exit 1
fi

SRC="$1"
DST="$2"

# Function to copy from a single source to destination
function copy_source() {
    local source="$1"
    local dest="$2"
    
    # Ensure destination directory exists (after validation)
    mkdir -p "$dest"
    
    # Check if source starts with s3://
    if [[ "$source" == s3://* || "$dest" == s3://* ]]; then
        # Use s5cmd to copy from S3
        time s5cmd cp "$source" "$dest"

        # If we copied a zip, extract it in place
        if [[ "$source" == s3://* && "$source" == *.zip ]]; then
            echo "Extracting zip file in place"
            time unzip "$dest"/*.zip -d "$dest"
            rm "$dest"/*.zip
        fi
    else
        # Use cp for local filesystem with recursive support
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

        echo "Checking if S3 path exists: $dst"
        if s5cmd ls "$dst" | grep -q .; then
            echo "Destination S3 path '$dst' already exists. Exiting."
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
