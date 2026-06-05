package main

import (
	"fmt"
	"os"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"

	"github.com/duck-labs/agentsdx/internal/datadir"
	"github.com/duck-labs/agentsdx/internal/profile"
	"github.com/duck-labs/agentsdx/internal/vm"
)

func main() {
	vaultSecret := os.Getenv("AGENTSDX_VAULT_SECRET")
	if vaultSecret == "" {
		fmt.Fprintln(os.Stderr, "error: AGENTSDX_VAULT_SECRET is not set")
		os.Exit(1)
	}

	if err := os.MkdirAll(datadir.ProfilesDir(), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "error: create profiles dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(datadir.VaultDir(), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "error: create vault dir: %v\n", err)
		os.Exit(1)
	}

	vmProviders := map[string]vm.VMProvider{}
	imageProviders := map[string]vm.ImageProvider{}

	if token := os.Getenv("AGENTSDX_HCLOUD_TOKEN"); token != "" {
		client := hcloud.NewClient(hcloud.WithToken(token))
		location := os.Getenv("AGENTSDX_HCLOUD_LOCATION")
		hp := vm.NewHetznerProvider(client, location)
		vmProviders["hetzner"] = hp
		imageProviders["hetzner"] = hp
	}

	lp := vm.NewLocalProvider(datadir.Dir())
	vmProviders["local"] = lp
	imageProviders["local"] = lp

	profiles := profile.NewStore(datadir.ProfilesDir())
	images := vm.NewImageStore(datadir.ImagesFile())

	root := &cobra.Command{
		Use:   "agentsdx",
		Short: "Manage agent sandboxes",
	}

	root.AddCommand(
		newProfilesCmd(profiles, images, vmProviders, imageProviders, vaultSecret),
		newSecretsCmd(vaultSecret),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
