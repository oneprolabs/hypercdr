package config

import "testing"

func TestDefaultImageSingleRepository(t *testing.T) {
	got := defaultImage("crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr", "comm-agent:dev")
	if got != "crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr:comm-agent-dev" {
		t.Fatalf("unexpected agent image: %s", got)
	}
}
