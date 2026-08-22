#!/usr/bin/env bash
# Print the next SemVer based on the latest v* git tag.
# Usage: next-version.sh [patch|minor|major]
set -euo pipefail

BUMP="${1:-minor}"

case "$BUMP" in
  patch | minor | major) ;;
  *)
    echo "error: bump must be patch, minor, or major (got: $BUMP)" >&2
    exit 1
    ;;
esac

LATEST_TAG="$(git tag -l 'v*' --sort=-v:refname | head -1 || true)"

if [[ -z "$LATEST_TAG" ]]; then
  echo "0.1.0"
  exit 0
fi

VERSION="${LATEST_TAG#v}"
BASE="${VERSION%%-*}"
IFS='.' read -r MAJOR MINOR PATCH <<< "$BASE"

MAJOR="${MAJOR:-0}"
MINOR="${MINOR:-0}"
PATCH="${PATCH:-0}"

case "$BUMP" in
  patch) PATCH=$((PATCH + 1)) ;;
  minor) MINOR=$((MINOR + 1)); PATCH=0 ;;
  major) MAJOR=$((MAJOR + 1)); MINOR=0; PATCH=0 ;;
esac

echo "${MAJOR}.${MINOR}.${PATCH}"
