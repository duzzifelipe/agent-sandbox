//go:build darwin && arm64

package main

import (
	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-server/internal/vm/applevz"
)

func newProvider(images *vm.ImageStore, isoDir, workDir string) vm.VMProvider {
	return applevz.NewProvider(images, isoDir, workDir)
}
