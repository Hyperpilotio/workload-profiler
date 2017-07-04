package main

import (
	"testing"
	"time"

	"github.com/hyperpilotio/blobstore"
	"github.com/magiconair/properties/assert"

	"github.com/spf13/viper"
)

var config *viper.Viper
var clusterStore blobstore.BlobStore

const (
	TEST_TEMPLATE_ID   = "analysis-base"
	TEST_DEPLOYMENT_ID = "workload-profiler-redis-12345678-1234"
	TEST_RUN_ID        = "analysis-base-12345678-1234-1234-abcd-a9496846b873"
)

func init() {
	config = viper.New()
	config.SetConfigType("json")
	config.SetConfigFile("./documents/deployed.config")
	config.ReadInConfig()

	clusterStore, _ = blobstore.NewBlobStore("Clusters", config)
}

func TestClusters(t *testing.T) {
	t.Run("Store Clusters", testStoreClusters)
	t.Run("Load Clusters", testLoadAllClusters)
	t.Run("Delete Clusters", testDeleteClusters)
}

func testStoreClusters(t *testing.T) {
	cluster := &StoreCluster{
		DeploymentTemplate: TEST_TEMPLATE_ID,
		DeploymentId:       TEST_DEPLOYMENT_ID,
		RunId:              TEST_RUN_ID,
		State:              GetStateString(AVAILABLE),
		Created:            time.Now().Format(time.RFC822),
	}

	if err := clusterStore.Store(cluster.RunId, cluster); err != nil {
		t.Errorf("Unable to store %s cluster: %s", TEST_TEMPLATE_ID, err.Error())
	}
}

func testLoadAllClusters(t *testing.T) {
	clusters, err := clusterStore.LoadAll(func() interface{} {
		return &StoreCluster{}
	})

	if err != nil {
		t.Errorf("Unable to load profiler clusters: %s", err.Error())
	}

	for _, cluster := range clusters.([]interface{}) {
		storeCluster := cluster.(*StoreCluster)
		if storeCluster.RunId == TEST_RUN_ID {
			assert.Equal(t, TEST_DEPLOYMENT_ID, storeCluster.DeploymentId)
			assert.Equal(t, TEST_TEMPLATE_ID, storeCluster.DeploymentTemplate)
		}
	}
}

func testDeleteClusters(t *testing.T) {
	if err := clusterStore.Delete(TEST_RUN_ID); err != nil {
		t.Errorf("Unable to delete profiler cluster: %s", err.Error())
	}
}
