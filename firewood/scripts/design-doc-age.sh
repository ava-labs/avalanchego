#!/usr/bin/env bash

set -euo pipefail

# Lists Firewood design docs by their last git-commit date, oldest (stalest) first.
#
# Motivation: freshness for a design doc comes from git history, never from a
# hand-maintained date written inside the document. The design-doc frontmatter
# schema deliberately has no date field, so a date in a doc would only drift out
# of sync with reality. This report is the working embodiment of that convention:
# it reads git alone — never a document's frontmatter or body — and surfaces the
# designs whose last commit is oldest, making an `active` design that has drifted
# from the code easy to spot.
#
# Scope: only the numbered design docs, docs/src/designs/NNNN-*.md. The section
# index (README.md) and the template (template.md) are not designs and are skipped.
#
# Ordering: oldest last-commit first, so the stalest designs sit at the top. A
# design with no commit yet (staged rename or untracked new file) has no git date
# and sorts last, labelled "(uncommitted)".
#
# Output: one row per design on stdout, "<YYYY-MM-DD>  <repo-relative-path>". A
# one-line legend is written to stderr so stdout stays pure data.
#
# Exit status: 0 on success (including when there are no design docs).

usage() {
    cat <<'EOF'
Usage: scripts/design-doc-age.sh [-h|--help]

Lists docs/src/designs/NNNN-*.md by last git-commit date, oldest first, so the
stalest designs appear at the top. Freshness is read from git history only, never
from a date inside a document. Designs with no commit yet sort last as
"(uncommitted)".

Exit status: 0 on success.
EOF
}

case "${1:-}" in
    -h | --help)
        usage
        exit 0
        ;;
    "") ;;
    *)
        echo "error: unexpected argument '$1'" >&2
        usage >&2
        exit 2
        ;;
esac

# Run from the repository root so globbing and pathspecs are repo-relative
# regardless of the caller's working directory.
cd "$(git rev-parse --show-toplevel)"

designs_dir="docs/src/designs"

if [[ ! -d "$designs_dir" ]]; then
    echo "error: designs directory not found: $designs_dir" >&2
    exit 1
fi

# A sort key that sorts after every real Unix timestamp (year ~2286), so
# uncommitted designs land at the bottom of the oldest-first listing.
uncommitted_key=9999999999

# Each row is "<sort-key>\t<display-date>\t<path>". The key is the committer date
# in Unix seconds; the display date is the same commit's date as YYYY-MM-DD.
rows=()
shopt -s nullglob
for doc in "$designs_dir"/[0-9][0-9][0-9][0-9]-*.md; do
    # -1: the most recent commit touching this path. %ct is the committer date in
    # Unix seconds (the sort key); %cd with --date=short is YYYY-MM-DD (display).
    # A path with no commit yet yields empty output (git log exits 0).
    meta=$(git log -1 --date=short --format='%ct%x09%cd' -- "$doc")
    if [[ -z "$meta" ]]; then
        rows+=("$uncommitted_key"$'\t'"(uncommitted)"$'\t'"$doc")
    else
        rows+=("${meta%%$'\t'*}"$'\t'"${meta#*$'\t'}"$'\t'"$doc")
    fi
done
shopt -u nullglob

if [[ ${#rows[@]} -eq 0 ]]; then
    echo "no design docs found under $designs_dir" >&2
    exit 0
fi

echo "design docs by last git-commit date (oldest first; freshness from git, not in-doc dates):" >&2

# Sort ascending by the numeric key (oldest first), ties broken by path for a
# stable listing, then drop the key and print "<date>  <path>".
printf '%s\n' "${rows[@]}" \
    | sort -t$'\t' -k1,1n -k3,3 \
    | cut -f2- \
    | while IFS=$'\t' read -r date doc; do
        printf '%-14s  %s\n' "$date" "$doc"
    done
