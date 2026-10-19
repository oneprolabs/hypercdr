// platform-configure-url is the offline recovery entry point for platform access.
package main

import (
	"context"
	"fmt"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/platformaccess"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: platform-configure-url https://app.example.com\nUses HCDR_DATABASE_URL. Updates take effect without restarting the API.")
		os.Exit(2)
	}
	if _, err := platformaccess.Normalize(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo, err := store.NewPostgresStore(ctx, os.Getenv("HCDR_DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot open platform database; check HCDR_DATABASE_URL.")
		os.Exit(1)
	}
	defer repo.Close()
	origin, err := platformaccess.Save(repo, os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Platform access address updated:", origin)
	fmt.Println("Google callback:", origin+"/api/v1/saas/auth/google/callback")
	fmt.Println("Existing agent connections are unchanged. Update external proxy/DNS/TLS and Google Console separately.")
}
