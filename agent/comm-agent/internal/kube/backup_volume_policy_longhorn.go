package kube

func adaptLonghornBackupVolume(parameters map[string]string) bool {
	if parameters["fromBackup"] != "" {
		return false
	}
	switch parameters["dataEngine"] {
	case "v1", "v2":
		parameters["numberOfReplicas"] = "1"
		return true
	default:
		return false
	}
}
