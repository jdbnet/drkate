#!/bin/bash

set -euo pipefail

cd web && npm ci && npm run build && cd ..

CGO_ENABLED=0 go build -ldflags="-s -w" -o drkate ./cmd/drkate
