package main

import "testing"

func TestResolveDeploymentLayout(t *testing.T) {
	t.Setenv("HCDR_PLATFORM_SERVICE", "enterprise-platform")
	combined := resolveDeploymentLayout("combined")
	if combined.apiKey != "PLATFORM_IMAGE" || combined.frontendKey != "PLATFORM_IMAGE" || combined.platformService != "enterprise-platform" || combined.frontendService != "" {
		t.Fatalf("unexpected combined layout: %#v", combined)
	}
	split := resolveDeploymentLayout("split")
	if split.apiKey != "PLATFORM_API_IMAGE" || split.frontendKey != "PLATFORM_FRONTEND_IMAGE" || split.platformService != "hypercdr-platform-api" || split.frontendService != "hypercdr-platform-frontend" {
		t.Fatalf("unexpected split layout: %#v", split)
	}
}

func TestComposeUsesConfiguredProjectAndFile(t *testing.T) {
	t.Setenv("HCDR_COMPOSE_PROJECT_NAME", "hypercdr-enterprise")
	t.Setenv("HCDR_COMPOSE_FILE", "compose.yaml")
	cmd := compose("/deploy", "up", "-d", "platform")
	want := []string{"docker", "compose", "--project-name", "hypercdr-enterprise", "--project-directory", "/deploy", "--env-file", "/deploy/.env", "-f", "/deploy/compose.yaml", "up", "-d", "platform"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("unexpected args: %#v", cmd.Args)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("arg %d = %q, want %q; all args: %#v", i, cmd.Args[i], want[i], cmd.Args)
		}
	}
}
