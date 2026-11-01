#!/usr/bin/bash
set -e
GOODPATH="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$GOODPATH/.."

# 1. Backend — check and build the Go binary into a temp file, so a failed
#    build never replaces the binary the service runs (./sloubi, see sloubi.service).
#    Frontend (views/, assets/) is static: nothing to build.
go mod tidy
go vet ./...
go build -ldflags "-s -w" -o sloubi.new .

# 2. Security scan — fail the build on any reachable Go vulnerability.
#    Install once with: go install golang.org/x/vuln/cmd/govulncheck@latest
GOVULNCHECK="$(command -v govulncheck || echo "$(go env GOPATH)/bin/govulncheck")"
if [ -x "$GOVULNCHECK" ]; then
  "$GOVULNCHECK" ./... || { rm -f sloubi.new; exit 1; }
else
  echo "WARN: govulncheck not installed — skipping Go vulnerability scan"
fi

# 3. Swap the binary and restart the service.
service sloubi stop
mv -f sloubi.new sloubi
service sloubi start
service sloubi status --no-pager | head -n 5
