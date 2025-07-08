#!/usr/bin/env bash

set -euo pipefail

# Usage: ./scripts/copy_dir.sh source_directory destination_directory
# Sources can be S3 URLs (s3://bucket/path) or a local file path
# Assumes s5cmd has been installed and is available in the PATH

if [ $# -ne 2 ]; then
    echo "Usage: $0 <source_directory> <destination_directory>"
    echo "S3 Example: $0 's3://bucket1/path1' /dest/dir"
    echo "Local Example: $0 '/local/path1' /dest/dir"
    exit 1
fi

SRC="$1"
DST="$2"

# Ensure destination directory exists
mkdir -p "$DST"

# Function to copy from a single source to destination
copy_source() {
    local source="$1"
    local dest="$2"
    
    # Check if source starts with s3://
    if [[ "$source" == s3://* ]]; then
        echo "Copying from S3: $source -> $dest"
        # Use s5cmd to copy from S3
        time s5cmd cp "$source" "$dest"

        # If we copied a zip, extract it in place
        if [[ "$source" == *.zip ]]; then
            echo "Extracting zip file in place"
            time unzip "$dest"/*.zip -d "$dest"
            rm "$dest"/*.zip
        fi
    else
        echo "Copying from local filesystem: $source -> $dest"
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

copy_source "$SRC" "$DST"
