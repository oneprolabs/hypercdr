// Package registration defines the wire types shared by the API and isolated executor.
package registration

type InspectRequest struct {
	SessionID   string `json:"sessionId"`
	Context     string `json:"context"`
	ClusterType string `json:"clusterType"`
}
type Inspection struct {
	Context             string           `json:"context"`
	ClusterName         string           `json:"clusterName"`
	ClusterID           string           `json:"clusterId"`
	Region              string           `json:"region,omitempty"`
	ServerVersion       string           `json:"serverVersion"`
	NodeCount           int              `json:"nodeCount"`
	StorageClasses      []string         `json:"storageClasses"`
	DefaultStorageClass string           `json:"defaultStorageClass,omitempty"`
	Gates               []InspectionGate `json:"gates"`
}
type InspectionGate struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}
