//go:build linux

package main

import "github.com/duck-labs/agentsdx-server/internal/vm"

func newProvider(images *vm.ImageStore, isoDir, workDir string) vm.VMProvider {
	return vm.NewVirtualBoxProvider(images, isoDir)
}
