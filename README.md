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

