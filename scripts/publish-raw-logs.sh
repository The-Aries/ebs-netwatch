#!/usr/bin/env bash
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

cd "$repo_root"

gocache="${GOCACHE:-/tmp/go-build-cache}"

GOCACHE="$gocache" go test ./...
GOCACHE="$gocache" go run ./cmd/ebs-netwatch -prepare-publish

mkdir -p docs/data

cp data/manifest.json docs/data/manifest.json

find docs/data -maxdepth 1 -type f -name 'checks-*.jsonl' -delete
find data -maxdepth 1 -type f -name 'checks-*.jsonl' -exec cp {} docs/data/ \;

# Restrict staging to raw log artifacts so automatic publish commits do not mix in functional changes.
staged_paths=()
for path in data/manifest.json docs/data/manifest.json; do
  [ -e "$path" ] && staged_paths+=("$path")
done
for path in docs/data/checks-*.jsonl; do
  [ -e "$path" ] && staged_paths+=("$path")
done

for path in data/checks-*.jsonl; do
  [ -e "$path" ] && staged_paths+=("$path")
done

while IFS= read -r path; do
  case "$path" in
    data/checks-*.jsonl|data/manifest.json|docs/data/checks-*.jsonl|docs/data/manifest.json)
      ;;
    "")
      ;;
    *)
      printf 'Refusing to publish with pre-staged non-log file: %s\n' "$path" >&2
      exit 1
      ;;
  esac
done < <(git diff --cached --name-only)

if [ "${#staged_paths[@]}" -gt 0 ]; then
  git add -- "${staged_paths[@]}"
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
