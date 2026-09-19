#!/bin/sh
set -eu

case "${1:-}" in
  generate|check) mode=$1 ;;
  *) printf '%s\n' 'Usage: sh scripts/api-types.sh generate|check' >&2; exit 2 ;;
esac

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TMP_DIR=$(mktemp -d)
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

cd "$ROOT"
go run ./cmd/prohibitorum openapi > "$TMP_DIR/openapi.yaml"
pnpm --dir dashboard --filter @prohibitorum/api-types exec openapi-typescript \
  "$TMP_DIR/openapi.yaml" --output "$TMP_DIR/schema.d.ts" --alphabetize

TARGET=dashboard/src/api/generated/schema.d.ts
if [ "$mode" = generate ]; then
  mkdir -p "$(dirname -- "$TARGET")"
  cp "$TMP_DIR/schema.d.ts" "$TARGET"
elif ! cmp -s "$TMP_DIR/schema.d.ts" "$TARGET"; then
  printf '%s\n' 'API declarations are out of date. Run: mise run dev:api-types' >&2
  diff -u "$TARGET" "$TMP_DIR/schema.d.ts" || true
  exit 1
fi
