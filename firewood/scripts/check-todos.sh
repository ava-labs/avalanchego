#!/usr/bin/env bash

set -euo pipefail

# Enforces that every TODO/FIXME has an attached owner or issue.
#
# Motivation (https://github.com/ava-labs/firewood/issues/1603): bare TODOs lose
# their context over time and have no backlog/grooming pressure behind them.
# Requiring each one to name an owner or link an issue keeps that context alive.
#
# Rule: a TODO or FIXME marker in a .rs or .go file must be annotated with an
# owner or GitHub issue inside parentheses immediately after the marker:
#
#   TODO(owner or GH issue) - e.g. TODO(#1603), TODO(@foo), FIXME(rust-lang/rust#143874)
#
# The Rust todo!() macro is held to the same rule: it must carry a non-empty
# message naming an owner or issue, e.g. todo!("#1603: implement this"). A bare
# todo!() is a violation.
#
# Any marker without a non-empty parenthesized annotation is a violation, e.g.:
#   TODO:  /  TODO foo:  /  TODO[foo]:  /  //TODO  /  todo!()
#
# The contents of the parentheses are not validated beyond being non-empty; the
# syntax can be tightened later. Comment markers are matched as whole, uppercase
# words anywhere on a line; the todo!() macro is matched in lowercase.
#
# Exit status: 0 when clean, 1 when any violation is found.

usage() {
    cat <<'EOF'
Usage: scripts/check-todos.sh [-h|--help]

Scans tracked .rs and .go files for TODO/FIXME markers and the Rust todo!()
macro, and reports any that are not annotated with an owner or GitHub issue.

A marker is valid when it is immediately followed by a non-empty parenthesized
annotation:

  TODO(owner or GH issue)   e.g. TODO(#1603), TODO(@foo), FIXME(rust-lang/rust#143874)
  todo!("...")              e.g. todo!("#1603: implement this")

Exit status: 0 when no violations, 1 otherwise.
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

# Run from the repository root so paths are repo-relative and the whole tree is
# covered regardless of the caller's working directory.
cd "$(git rev-parse --show-toplevel)"

# A whole-word TODO/FIXME marker (not preceded/followed by an identifier char).
marker_re='(^|[^A-Za-z0-9_])(TODO|FIXME)([^A-Za-z0-9_]|$)'

# A validly-annotated marker: TODO/FIXME immediately followed by a non-empty
# parenthesized annotation (an owner or GitHub issue). The contents are not
# validated further for now — the syntax can be tightened later.
valid_re='(^|[^A-Za-z0-9_])(TODO|FIXME)\([^)]*[^)[:space:]][^)]*\)'

# The Rust todo!() macro, held to the same rule. The marker is a lowercase
# todo! invocation; it is valid only when followed by a non-empty parenthesized
# message (which should name an owner or issue). A bare todo!() is a violation.
macro_marker_re='(^|[^A-Za-z0-9_])todo!'
macro_valid_re='(^|[^A-Za-z0-9_])todo![[:space:]]*\([^)]*[^)[:space:]][^)]*\)'

# git grep skips target/, untracked, and gitignored files for us. It exits 1
# when there are no matches, which is not an error for us.
candidates=$(git grep -nE 'TODO|FIXME|todo!' -- '*.rs' '*.go' || true)

violations=0
while IFS= read -r match; do
    [ -n "$match" ] || continue

    file=${match%%:*}
    rest=${match#*:}
    lineno=${rest%%:*}
    content=${rest#*:}

    # A line is flagged if it carries an unannotated comment marker or an
    # unannotated todo!() macro. Substring-only hits (e.g. MYTODO) match neither.
    flagged=0
    if [[ $content =~ $marker_re ]] && ! [[ $content =~ $valid_re ]]; then
        flagged=1
    fi
    if [[ $content =~ $macro_marker_re ]] && ! [[ $content =~ $macro_valid_re ]]; then
        flagged=1
    fi
    [ "$flagged" -eq 1 ] || continue

    trimmed=${content#"${content%%[![:space:]]*}"}
    echo "${file}:${lineno}: ${trimmed}"
    violations=$((violations + 1))
done <<< "$candidates"

if [ "$violations" -eq 0 ]; then
    echo "No TODO/FIXME violations found."
    exit 0
fi

echo ""
echo "Found ${violations} TODO/FIXME violation(s)."
echo "Each TODO/FIXME must be annotated as TODO(owner or GH issue), e.g. TODO(#1603) or TODO(@foo)."
exit 1
