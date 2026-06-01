#!/usr/bin/env bash
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

cd "$repo_root"

gocache="${GOCACHE:-/tmp/go-build-cache}"

GOCACHE="$gocache" go test ./...
GOCACHE="$gocache" go run ./cmd/ebs-netwatch -prepare-publish

rm -rf docs
mkdir -p docs/data

cp web/index.html docs/index.html
cp web/style.css docs/style.css
cp web/app.js docs/app.js
cp data/manifest.json docs/data/manifest.json

find data -maxdepth 1 -type f -name 'checks-*.jsonl' -exec cp {} docs/data/ \;

raw_logs=(data/checks-*.jsonl)

git add docs data/manifest.json
if [ -e "${raw_logs[0]}" ]; then
  git add "${raw_logs[@]}"
fi

if git diff --cached --quiet; then
  exit 0
fi

git commit -m "update network status logs"

if git rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1; then
  git push
else
  branch="$(git branch --show-current)"
  git push --set-upstream origin "$branch"
fi
