package config

import "testing"

func TestDefaultImageSingleRepository(t *testing.T) {
	got := defaultImage("registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr", "comm-agent:dev")
	if got != "registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:comm-agent-dev" {
		t.Fatalf("unexpected agent image: %s", got)
	}
}
