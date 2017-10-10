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
