curl -XPOST -H "Content-Type: application/json" localhost:7779/benchmarks/kafka -d "{\"deploymentId\":\"$1\", \"startingIntensity\":10, \"step\":10, \"sloTolerance\":0.1}"
