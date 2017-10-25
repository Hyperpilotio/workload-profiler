#!/bin/bash

if [ "$#" -ne 1 ]
then
    echo "Usage: clusterMetric.sh <deploymentId>"
    exit 1
fi

DEPLOYER_URL="localhost"
WORKLOAD_PROFILER_URL=$(curl -s $DEPLOYER_URL:7777/v1/deployments/$1/services | jq '.data' | jq 'with_entries(select(.key=="workload-profiler"))' | jq '.[].publicUrl')
WORKLOAD_PROFILER_URL=${WORKLOAD_PROFILER_URL//\"/}

APP_NAME="resource-worker-service"
curl -XPOST -H "Content-Type:application/json" \
$WORKLOAD_PROFILER_URL/clusterMetrics/apps/$APP_NAME \
--data @<(cat <<EOF
{
    "loadTesters" : [
        {
            "slowCookerController" : {
                "calibrate" : {
                    "step" : 5,
                    "initialConcurrency" : 40,
                    "runsPerIntensity" : 3
                },
                "appLoad" : {
                    "url" : "http://resource-worker-0.default:7998/run",
                    "qps" : 5,
                    "data" : "@/etc/request_bodies",
                    "method" : "POST",
                    "totalRequests" : 100000
                },
                "loadTime" : "30s"
            },
            "name" : "slow-cooker"
        }
    ],
    "duration" : "30s"
}
EOF
)

