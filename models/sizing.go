package models

type AWSCost struct {
	LinuxOnDemand   float32 `bson:"linuxOnDemand" json:"linuxOnDemand", binding:"required"`
	LinuxReserved   float32 `bson:"linuxReserved" json:"linuxReserved", binding:"required"`
	WindowsOnDemand float32 `bson:"windowsOnDemand" json:"windowsOnDemand", binding:"required"`
	WindowsReserved float32 `bson:"windowsReserved" json:"windowsReserved", binding:"required"`
}

type AWSCPUConfig struct {
	VCPU       int     `bson:"vcpu" json:"vcpu", binding:"required"`
	CpuType    string  `bson:"cpuType" json:"cpuType", binding:"required"`
	ClockSpeed float32 `bson:"clockSpeed" json:"clockSpeed", binding:"required"`
}

type AWSMemoryConfig struct {
	Size float32 `bson:"size" json:"size", binding:"required"`
}

type AWSNetworkConfig struct {
	NetworkType       string  `bson:"networkType" json:"networkType", binding:"required"`
	Bandwidth         float32 `bson:"bandwidth" json:"bandwidth"`
	EnhanceNetworking bool    `bson:"enhancedNetworking" json:"enhancedNetworking"`
}

type AWSStorageConfig struct {
	Size        float32 `bson:"size" json:"size"`
	StorageType string  `bson:"storageType" json:"storageType", binding:"required"`
	Bandwidth   float32 `bson:"bandwidth" json:"bandwidth"`
}

type AWSNodeType struct {
	Name           string           `bson:"name" json:"name", binding:"required"`
	instanceFamily string           `bson:"instanceFamily" json:"instanceFamily", binding:"required"`
	Category       string           `bson:"category" json:"category", binding:"required"`
	HourlyCost     AWSCost          `bson:"cost" json:"cost", binding:"required"`
	CpuConfig      AWSCPUConfig     `bson:"cpuConfig" json:"cpuConfig", binding:"required"`
	MemoryConfig   AWSMemoryConfig  `bson:"memoryConfig" json:"memoryConfig", binding:"required"`
	NetworkConfig  AWSNetworkConfig `bson:"networkConfig" json:"networkConfig", binding:"required"`
	StorageConfig  AWSStorageConfig `bson:"storageConfig" json:"storageConfig", binding:"required"`
}

type NetUsage struct {
	BytesReceivedPerSec int `bson:"bytesReceivedPerSec" json:"bytesReceivedPerSec", binding:"required"`
	BytesSentPerSec     int `bson:"bytesSentPerSec" json:"bytesSentPerSec", binding:"required"`
}

type DiskUsage struct {
	BytesReadPerSec  int `bson:"bytesReadPerSec" json:"bytesReadPerSec", binding:"required"`
	BytesWritePerSec int `bson:"bytesWritePerSec" json:"bytesWritePerSec", binding:"required"`
	OpsReadPerSec    int `bson:"OpsReadPerSec" json:"OpsReadPerSec", binding:"required"`
	OpsWritePerSec   int `bson:"OpsWritePerSec" json:"OpsWritePerSec", binding:"required"`
}

type ResourceUsage struct {
	CpuUtilization float64   `bson:"cpuUtilization" json:"cpuUtilization", binding:"required"`
	MemUsage       float64   `bson:"memUsage" json:"memUsage", binding:"required"`
	NetUsage       NetUsage  `bson:"netUsage" json:"netUsage", binding:"required"`
	DiskUsage      DiskUsage `bson:"diskUsage" json:"diskUsage", binding:"required"`
}

type SizingResults struct {
	RunId     string `bson:"runID" json:"runId", binding:"required"`
	Duration  int    `bson:"duration" json:"duration", binding:"required"`
	AppName   string `bson:"appName" json:"appName", binding:"required"`
	SloResult SLO    `bson:"sloResult" json:"sloResult", binding:"required"`
}
