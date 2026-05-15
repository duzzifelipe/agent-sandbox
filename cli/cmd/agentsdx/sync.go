package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/duck-labs/agentsdx-cli/internal/state"
	"github.com/spf13/cobra"
)

func newSyncCmd(c *client.Client, s *state.State) *cobra.Command {
	return &cobra.Command{
		Use:   "sync <profile> <file>",
		Short: "Push a local file into a running VM",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := args[0]
			localFile := args[1]

			sessionID, ok := s.Get(profile)
			if !ok {
				return fmt.Errorf("no active session found for profile %q", profile)
			}

			session, err := c.GetSession(sessionID)
			if err != nil {
				return fmt.Errorf("get session: %w", err)
			}
			if session.IPAddress == "" {
				return fmt.Errorf("session has no IP address (state: %s)", session.State)
			}

			privateKey, err := c.GetSessionKey(sessionID)
			if err != nil {
				return fmt.Errorf("get session key: %w", err)
			}

			keyFile, err := os.CreateTemp("", "agentsdx-key-*")
			if err != nil {
				return fmt.Errorf("create temp key file: %w", err)
			}
			defer os.Remove(keyFile.Name())

			if _, err := keyFile.WriteString(privateKey); err != nil {
				return fmt.Errorf("write key: %w", err)
			}
			keyFile.Close()

			if err := os.Chmod(keyFile.Name(), 0600); err != nil {
				return fmt.Errorf("chmod key: %w", err)
			}

			scpBin, err := exec.LookPath("scp")
			if err != nil {
				return fmt.Errorf("scp not found in PATH: %w", err)
			}

			remoteDest := fmt.Sprintf("root@%s:~/%s", session.IPAddress, filepath.Base(localFile))
			scpArgs := []string{
				"scp",
				"-i", keyFile.Name(),
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				localFile,
				remoteDest,
			}

			fmt.Printf("Copying %s → %s\n", localFile, remoteDest)
			return syscall.Exec(scpBin, scpArgs, os.Environ())
		},
	}
}
