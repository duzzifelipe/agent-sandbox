package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"

	"github.com/duck-labs/agentsdx-server/internal/api"
	"github.com/duck-labs/agentsdx-server/internal/builder"
	"github.com/duck-labs/agentsdx-server/internal/db"
	"github.com/duck-labs/agentsdx-server/internal/profile"
	"github.com/duck-labs/agentsdx-server/internal/session"
	"github.com/duck-labs/agentsdx-server/internal/vm"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server",
		Run: func(cmd *cobra.Command, args []string) {
			runServe()
		},
	}
}

func runServe() {
	secret := mustEnv("AGENTSDX_VAULT_SECRET")
	hcloudToken := os.Getenv("AGENTSDX_HCLOUD_TOKEN")
	dataDir := envOrDefault("AGENTSDX_DATA_DIR", "./data")
	addr := envOrDefault("AGENTSDX_ADDR", ":8080")
	serverURL := envOrDefault("AGENTSDX_SERVER_URL", "http://localhost"+addr)
	vmDir := envOrDefault("AGENTSDX_VM_DIR", "./vm")
	hcloudLocation := envOrDefault("AGENTSDX_HCLOUD_LOCATION", "nbg1")

	for _, dir := range []struct {
		path string
		mode os.FileMode
	}{
		{filepath.Join(dataDir, "profiles"), 0755},
		{filepath.Join(dataDir, "vault"), 0700},
		{filepath.Join(dataDir, "images"), 0755},
		{filepath.Join(dataDir, "qemu"), 0755},
		{filepath.Join(dataDir, "qemu", "cache"), 0755},
		{filepath.Join(dataDir, "qemu", "snapshots"), 0755},
		{filepath.Join(dataDir, "qemu", "sessions"), 0755},
	} {
		if err := os.MkdirAll(dir.path, dir.mode); err != nil {
			log.Fatalf("create data dir %s: %v", dir.path, err)
		}
	}

	conn, err := db.Open(filepath.Join(dataDir, "agentsdx.db"))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer conn.Close()

	if err := db.MigrateYAMLProfiles(conn, filepath.Join(dataDir, "profiles")); err != nil {
		log.Printf("WARN: YAML profile migration: %v", err)
	}

	localProvider := vm.NewLocalProvider(conn, filepath.Join(dataDir, "qemu"))

	vmProviders := map[string]vm.VMProvider{"local": localProvider}
	imgProviders := map[string]vm.ImageProvider{"local": localProvider}

	if hcloudToken != "" {
		client := hcloud.NewClient(hcloud.WithToken(hcloudToken))
		hetznerProvider := vm.NewHetznerProvider(client, hcloudLocation)
		vmProviders["hetzner"] = hetznerProvider
		imgProviders["hetzner"] = hetznerProvider
	}

	images := vm.NewImageStore(filepath.Join(dataDir, "images.json"))
	profileStore := profile.NewStore(conn, filepath.Join(dataDir, "profiles"))
	sessionStore := session.NewStore(conn)
	mgr := session.NewManager(sessionStore, vmProviders, images, filepath.Join(dataDir, "vault"), secret, serverURL)
	imageBuilder := builder.New(vmDir, images, imgProviders)

	h := api.NewHandler(profileStore, mgr, imageBuilder, filepath.Join(dataDir, "vault"), secret)

	srv := &http.Server{Addr: addr, Handler: h.Router()}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("agentsdxd listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-quit
	log.Println("agentsdxd shutting down — stopping active sessions")
	mgr.StopAll(context.Background())

	if err := srv.Shutdown(context.Background()); err != nil {
		log.Printf("http shutdown: %v", err)
	}
}
