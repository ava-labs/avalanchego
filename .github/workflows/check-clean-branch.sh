#!/bin/bash
# Exits if any uncommitted changes are found.

git update-index --really-refresh >> /dev/null
git diff-index --quiet HEAD
