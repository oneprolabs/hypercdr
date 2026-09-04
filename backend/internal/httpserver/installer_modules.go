package httpserver

import (
	"embed"
	"fmt"
	"strings"
)

// installerModuleFS keeps provider implementations as independently reviewed
// shell sources while /install.sh remains a single, fast user-facing download.
//
//go:embed installers/*.sh
var installerModuleFS embed.FS

var installerModuleOrder = []string{
	"installers/core-preflight.sh",
	"installers/scenario-contract.sh",
	"installers/provider-contract.sh",
	"installers/native-kubernetes.sh",
	"installers/huaweicloud-cce.sh",
	"installers/openshift.sh",
}

func assembledInstallerModules() (string, error) {
	var result strings.Builder
	for _, name := range installerModuleOrder {
		content, err := installerModuleFS.ReadFile(name)
		if err != nil {
			return "", fmt.Errorf("read installer module %s: %w", name, err)
		}
		result.Write(content)
		result.WriteString("\n")
	}
	return result.String(), nil
}
