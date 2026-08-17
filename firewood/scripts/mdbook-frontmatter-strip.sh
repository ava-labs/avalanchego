#!/usr/bin/env bash

set -euo pipefail

# mdBook preprocessor that strips a leading YAML frontmatter block (--- ... ---)
# from every chapter before rendering, so the design-doc frontmatter schema
# (title/status/category/authors/tracking-issue) does not surface in the rendered
# HTML as a stray horizontal rule followed by a heading.
#
# Why in-house rather than a published preprocessor: the equivalent crates are
# single-maintainer projects with no prebuilt binary, so they compile from source
# on every CI run and would block all docs/Go PRs if the crate were yanked or went
# unmaintained. The transform is a few lines of jq, so the project owns it here.
# See docs/src/designs/0001-mdbook-documentation-site.md.
#
# Preprocessor protocol
# (https://rust-lang.github.io/mdBook/for_developers/preprocessors.html):
#   - mdBook first calls `<cmd> supports <renderer>`; exit 0 to claim support.
#     This stripper is renderer-agnostic, so it supports every renderer.
#   - Otherwise mdBook sends `[context, book]` as JSON on stdin and expects the
#     transformed `book` object back on stdout.

# Renderer-support probe: strip frontmatter for every renderer (html, linkcheck2, …).
if [[ "${1:-}" == "supports" ]]; then
    exit 0
fi

# Transform the book. `walk` visits every node, so it reaches chapters at any depth
# (including nested `sub_items`) and is agnostic to whether the book's item list is
# keyed `items` (mdBook 0.5) or `sections` (older) — it only rewrites `content`
# strings. A chapter's content is stripped of a leading `--- ... ---` block. jq's
# Oniguruma flags: `s` anchors `^` to the string start (not each line) and `m` lets
# `.` span newlines (Oniguruma "multi line"); with `.*?` the match runs from the
# opening fence to the first closing fence only.
jq -c '
  def strip_frontmatter:
    sub("^---[ \t]*\r?\n.*?\r?\n---[ \t]*\r?\n"; ""; "sm");
  .[1] | walk(
    if type == "object" and has("content") and (.content | type) == "string"
    then .content |= strip_frontmatter
    else . end
  )
'
