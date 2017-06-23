# workload-profiler
A service that orchestrates various pieces to run workload profiling

## Use
1. create a test cluster first by running the following for a target app (e.g., redis, mongo), and get the $DeploymentId
	```{shell}
	./deploy-k8s.sh
	```
2. create deployed.config by:
	```{shell}
	cd documents/template.config documents/deployed.config
	vi documents/deployed.config
	```
3. start workload-profiler service by:
	```{shell}
	./run.sh
	```
4. run a calibration test by:
	```{shell}
	./calibrate.sh $DeploymentId
	```
5. run a benchmarking test by:
	```{shell}
	vi benchmark.sh
	./benchmark.sh $DeploymentId
	```
