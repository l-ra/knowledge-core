#!/usr/bin/env bash
# Set Helm chart version and appVersion in Chart.yaml.
# Usage: set-chart-version.sh <version> [chart.yaml]
set -euo pipefail

VERSION="${1:?usage: set-chart-version.sh <version> [chart.yaml]}"
CHART_FILE="${2:-deploy/helm/knowledge-core/Chart.yaml}"

VERSION="${VERSION#v}"

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]; then
  echo "error: version must be valid SemVer (got: $VERSION)" >&2
  exit 1
fi

if [[ ! -f "$CHART_FILE" ]]; then
  echo "error: chart file not found: $CHART_FILE" >&2
  exit 1
fi

sed -i "s/^version: .*/version: ${VERSION}/" "$CHART_FILE"
sed -i "s/^appVersion: .*/appVersion: \"${VERSION}\"/" "$CHART_FILE"

echo "Set ${CHART_FILE} -> version=${VERSION}, appVersion=${VERSION}"
