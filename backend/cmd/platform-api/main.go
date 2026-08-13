package main

import (
	"log"

	"hypercdr-platform/platform/backend/pkg/platform"
)

func main() {
	if err := platform.Run(platform.CommunityOptions()); err != nil {
		log.Fatal(err)
	}
}
