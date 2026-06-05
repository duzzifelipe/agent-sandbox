package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/duck-labs/agentsdx/internal/types"
	"github.com/duck-labs/agentsdx/internal/vm"
)

// Run creates a VM for the profile, polls until SSH is ready, spawns an SSH
// child process, and destroys the VM when the child exits or a signal is received.
func Run(ctx context.Context, spec types.ProfileSpec, vaultData types.VaultData, provider vm.VMProvider, images *vm.ImageStore) error {
	imageID, err := images.GetImageID(vm.Provider(spec.Infrastructure.Provider), spec.Name)
	if err != nil {
		return err
	}

	userData := vm.BuildUserData(
		vaultData.VMAccessPublicKey,
		vaultData.GitPrivateKey,
		spec.Name,
		vaultData.Secrets,
		spec.Projects,
	)

	fmt.Printf("Starting VM for profile %q...\n", spec.Name)
	v, err := provider.CreateVM(ctx, vm.CreateVMRequest{
		ProfileName:   spec.Name,
		ImageID:       imageID,
		AuthorizedKey: vaultData.VMAccessPublicKey,
		UserData:      userData,
	})
	if err != nil {
		return fmt.Errorf("create vm: %w", err)
	}

	defer func() {
		fmt.Println("\nDestroying VM...")
		_ = provider.DestroyVM(context.Background(), v.ID)
	}()

	fmt.Printf("Waiting for VM to be ready")
	if err := pollUntilRunning(ctx, provider, v); err != nil {
		return err
	}
	fmt.Println()

	keyFile, err := os.CreateTemp("", "agentsdx-key-*")
	if err != nil {
		return fmt.Errorf("create temp key file: %w", err)
	}
	defer os.Remove(keyFile.Name())

	if _, err := keyFile.WriteString(vaultData.VMAccessPrivateKey); err != nil {
		return fmt.Errorf("write key: %w", err)
	}
	keyFile.Close()
	if err := os.Chmod(keyFile.Name(), 0600); err != nil {
		return fmt.Errorf("chmod key: %w", err)
	}

	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("ssh not found in PATH: %w", err)
	}

	sshArgs := []string{
		sshBin,
		"-i", keyFile.Name(),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-t",
	}
	if v.SSHPort != 0 {
		sshArgs = append(sshArgs, "-p", fmt.Sprintf("%d", v.SSHPort))
	}
	sshArgs = append(sshArgs, fmt.Sprintf("ubuntu@%s", v.IPAddress), "/usr/local/bin/entrypoint.sh")

	fmt.Printf("Connecting to %s...\n", v.IPAddress)
	sshCmd := exec.Command(sshArgs[0], sshArgs[1:]...)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	if err := sshCmd.Start(); err != nil {
		return fmt.Errorf("start ssh: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	doneCh := make(chan error, 1)
	go func() { doneCh <- sshCmd.Wait() }()

	select {
	case <-sigCh:
		_ = sshCmd.Process.Signal(syscall.SIGTERM)
		<-doneCh
	case <-doneCh:
	}

	return nil
}

func pollUntilRunning(ctx context.Context, provider vm.VMProvider, v *vm.VM) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		current, err := provider.GetVM(ctx, v.ID)
		if err == nil && current.State == vm.VMStateRunning {
			v.IPAddress = current.IPAddress
			v.SSHPort = current.SSHPort
			return nil
		}
		fmt.Print(".")
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("VM did not become ready within 2 minutes")
}
