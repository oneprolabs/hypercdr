package httpserver

type recoveryTaskRequest struct {
	ClusterID                  string            `json:"clusterId"`
	ProtectionPlanID           string            `json:"protectionPlanId"`
	RestorePointID             string            `json:"restorePointId"`
	VeleroBackupName           string            `json:"veleroBackupName"`
	StorageRepo                string            `json:"storageRepo"`
	SourceNamespace            string            `json:"sourceNamespace"`
	SourceNamespaces           []string          `json:"sourceNamespaces"`
	TargetNamespace            string            `json:"targetNamespace"`
	TargetNamespaces           map[string]string `json:"targetNamespaces"`
	NamespaceMode              string            `json:"namespaceMode"`
	TargetMode                 string            `json:"targetMode"`
	RestoreMode                string            `json:"restoreMode"`
	ArtifactMode               string            `json:"artifactMode"`
	ConflictPolicy             string            `json:"conflictPolicy"`
	OriginalNamespaceConfirmed bool              `json:"originalNamespaceConfirmed"`
	IncludeClusterScoped       bool              `json:"includeClusterScoped"`
	UseTransforms              bool              `json:"useTransforms"`
	TransformPreset            string            `json:"transformPreset"`
	StorageProfileMode         string            `json:"storageProfileMode"`
	AlternateProfileID         string            `json:"alternateProfileId"`
	IncludedResources          []string          `json:"includedResources"`
	ExcludedResources          []string          `json:"excludedResources"`
	StorageClassMappings       map[string]string `json:"storageClassMappings"`
	ImageMappings              map[string]string `json:"imageMappings"`
	ServiceNodePortMappings    map[string]int    `json:"serviceNodePortMappings"`
	WaitForWorkloads           *bool             `json:"waitForWorkloads"`
	RunValidation              *bool             `json:"runValidation"`
	ForceStart                 bool              `json:"forceStart"`
	ContentCatalogLoaded       bool              `json:"contentCatalogLoaded"`
	PersistentDataExpected     bool              `json:"persistentDataExpected"`
	ReadinessExpectationsKnown bool              `json:"readinessExpectationsKnown"`
	RuntimeWorkloadsExpected   bool              `json:"runtimeWorkloadsExpected"`
	ExpectedPVCs               []string          `json:"expectedPvcs"`
}
