package main

import "testing"

func TestPlatformComponentImage(t *testing.T) {
	got, err := platformComponentImage("registry.example/hypercdr/platform-api:v20260723.6", "platform-upgrader", "v20260723.6")
	if err != nil {
		t.Fatalf("derive image: %v", err)
	}
	const want = "registry.example/hypercdr/platform-upgrader:v20260723.6"
	if got != want {
		t.Fatalf("image = %q, want %q", got, want)
	}
}

func TestPlatformComponentImageRejectsInvalidAPIImage(t *testing.T) {
	if _, err := platformComponentImage("platform-api:v1", "platform-upgrader", "v1"); err == nil {
		t.Fatal("expected invalid API image to be rejected")
	}
}
