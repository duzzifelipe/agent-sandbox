package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

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
	dataDir := envOrDefault("AGENTSDX_DATA_DIR", "./data")
	addr := envOrDefault("AGENTSDX_ADDR", ":8080")
	serverURL := envOrDefault("AGENTSDX_SERVER_URL", "http://localhost"+addr)
	vmDir := envOrDefault("AGENTSDX_VM_DIR", "./vm")

	for _, dir := range []struct {
		path string
		mode os.FileMode
	}{
		{filepath.Join(dataDir, "profiles"), 0755},
		{filepath.Join(dataDir, "vault"), 0700},
		{filepath.Join(dataDir, "iso"), 0755},
		{filepath.Join(dataDir, "images"), 0755},
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

	profileStore := profile.NewStore(conn, filepath.Join(dataDir, "profiles"))
	images := vm.NewImageStore(filepath.Join(dataDir, "images.json"))
	provider := vm.NewVirtualBoxProvider(images, filepath.Join(dataDir, "iso"))

	sessionStore := session.NewStore(conn)
	mgr := session.NewManager(sessionStore, provider, filepath.Join(dataDir, "vault"), secret, serverURL)

	imageBuilder := builder.New(vmDir, filepath.Join(dataDir, "images"), images)

	h := api.NewHandler(profileStore, mgr, images, imageBuilder, filepath.Join(dataDir, "vault"), secret)

	log.Printf("agentsdxd listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Router()); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
