package registration

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// TaskTimeout is shared by credential retention and the isolated installer.
func TaskTimeout(clusterType string) time.Duration {
	if strings.TrimSpace(clusterType) == "openshift" {
		if seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv("HCDR_OPENSHIFT_REGISTRATION_TIMEOUT_SECONDS"))); err == nil && seconds >= 60 {
			return time.Duration(seconds) * time.Second
		}
		return 35 * time.Minute
	}
	return 20 * time.Minute
}
