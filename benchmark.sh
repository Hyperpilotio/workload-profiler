#!/usr/bin/env bash
curl -XPOST -H "Content-Type: application/json" localhost:7779/benchmarks/redis -d "{\"deploymentId\":\"$1\", \"startingIntensity\":100, \"step\": 10, \"sloTolerance\": 0.1}"
