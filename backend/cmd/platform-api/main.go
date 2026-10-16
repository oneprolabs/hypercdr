package main

import (
	"context"
	"hypercdr-platform/platform/backend/pkg/saas"
	"log"
	"os"
	"time"

	"hypercdr-platform/platform/backend/pkg/platform"
)

func main() {
	options := platform.CommunityOptions()
	cfg, err := saas.ConfigFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	service, closeService, err := saas.Open(ctx, os.Getenv("HCDR_DATABASE_URL"), cfg)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	defer closeService()
	options.EmailBinder = service.BindSystemEmail
	options.Routes = append(options.Routes, service.Routes()...)
	maintenance, stop := context.WithCancel(context.Background())
	defer stop()
	go service.Maintain(maintenance)
	if err := platform.Run(options); err != nil {
		log.Fatal(err)
	}
}
