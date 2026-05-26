package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/duck-labs/agentsdx-cli/internal/state"
	"github.com/duck-labs/agentsdx-shared/types"
	"github.com/spf13/cobra"
)

func newRunCmd(c *client.Client, s *state.State) *cobra.Command {
	return &cobra.Command{
		Use:   "run <profile>",
		Short: "Start a sandbox session and open an SSH connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := args[0]

			fmt.Printf("Starting session for profile %q...\n", profile)
			session, err := c.CreateSession(profile)
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}

			if err := s.Set(profile, session.ID); err != nil {
				return fmt.Errorf("save session state: %w", err)
			}

			fmt.Printf("Session %s created. Waiting for VM to start", session.ID)
			session, err = waitForRunning(c, session.ID)
			if err != nil {
				return err
			}
			fmt.Println()

			fmt.Printf("VM running at %s. Fetching SSH key...\n", session.IPAddress)
			privateKey, err := c.GetSessionKey(session.ID)
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

			sshBin, err := exec.LookPath("ssh")
			if err != nil {
				return fmt.Errorf("ssh not found in PATH: %w", err)
			}

			sshArgs := []string{
				"ssh",
				"-i", keyFile.Name(),
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"-t",
			}
			if session.SSHPort != 0 {
				sshArgs = append(sshArgs, "-p", fmt.Sprintf("%d", session.SSHPort))
			}
			sshArgs = append(sshArgs,
				fmt.Sprintf("root@%s", session.IPAddress),
				"/usr/local/bin/entrypoint.sh",
			)

			fmt.Printf("Connecting: %s\n", sshArgs[len(sshArgs)-1])
			return syscall.Exec(sshBin, sshArgs, os.Environ())
		},
	}
}

func waitForRunning(c *client.Client, sessionID string) (types.SessionResponse, error) {
	for {
		session, err := c.GetSession(sessionID)
		if err != nil {
			return session, fmt.Errorf("poll session: %w", err)
		}
		switch session.State {
		case types.SessionStateRunning:
			return session, nil
		case types.SessionStateDestroyed, types.SessionStateStopping:
			return session, fmt.Errorf("session ended unexpectedly with state %q", session.State)
		default:
			fmt.Print(".")
			time.Sleep(3 * time.Second)
		}
	}
}
