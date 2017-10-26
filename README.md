# workload-profiler
A service that orchestrates various pieces to run workload profiling

## Use

1. create a test cluster first by running the following for a target app (e.g., redis, mongo)
    Plase check the Makefile in the [hyperpilot-demo/workloads/{TARGET APP}](https://github.com/Hyperpilotio/hyperpilot-demo/tree/master/workloads)

2. create deployed.config by:
	```{shell}
	cd documents/template.config documents/deployed.config
	vi documents/deployed.config
	```
3. start workload-profiler service by:
	```{shell}
	./run.sh
	```

## Capture Cluster Metric Use on GCP

1. create a GCP in-cluster cluster first.
    Plase copy gcpServiceAccount.json into [hyperpilot-demo/workloads/in-cluster](https://github.com/Hyperpilotio/hyperpilot-demo/tree/master/workloads/in-cluster/deploy-gcp.json)

2. import data to mongo configdb:
    * Get mongo-serve public url
    ($DEPLOYER_URL:7777/v1/deployments/$DEPLOYMENT_ID/services)
	```{shell}
	python collect_applications.py MONGO_URL
	python collect_benchmarks.py MONGO_URL
	```
3. run clusterMetrics api by:
    Change clusterMetrics.sh request json
	```{shell}
	./clusterMetrics.sh <deploymentId>
	```    

## Job Workflow

Workload profiler
------------------

    HTTP client -> curl -XPOST localhost:7777/...../benchmarks/....
                   v
    API -> HTTP Requests
       - Serialize the HTTP Request
       - Create jobs
       - Submit multiple jobs to the Job Manager
                   v
    Job Manager
       - Pops job off the queue
       - Worker take the job, first create a cluster calling the Deployer. Application Config's template + task defitions -> deploy-k8s.json
       - Waits until the Deployer finishes launching the cluster
       - Calls the job's Run method
                   v
    Job (SingleInfluxBenchmarkRun)
       - Calls the Run method logic, calling individual services launched in the cluster through HTTP calls
