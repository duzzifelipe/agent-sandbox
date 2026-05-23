package builder

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshConn is a narrow interface over *ssh.Client used by provisioning helpers.
type sshConn interface {
	NewSession() (*ssh.Session, error)
	Close() error
}

// dialSSHWithRetry dials addr (host:port) retrying every 5s until ctx is cancelled.
func dialSSHWithRetry(ctx context.Context, addr, privKey string) (sshConn, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privKey))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	cfg := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec — ephemeral build VM
		Timeout:         10 * time.Second,
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var lastErr error
	for {
		c, err := ssh.Dial("tcp", addr, cfg)
		if err == nil {
			return c, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out connecting to %s: %w", addr, lastErr)
		case <-ticker.C:
		}
	}
}

// uploadDir tars localDir and extracts it into /tmp/agentsdx-vm/ on the remote.
func uploadDir(conn sshConn, localDir string) error {
	session, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	pr, pw := io.Pipe()
	session.Stdin = pr
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	errCh := make(chan error, 1)
	go func() {
		gw := gzip.NewWriter(pw)
		tw := tar.NewWriter(gw)
		walkErr := filepath.WalkDir(localDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(localDir, path)
			if rel == "." {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = rel
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if !d.IsDir() {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()
				_, err = io.Copy(tw, f)
				return err
			}
			return nil
		})
		tw.Close()
		gw.Close()
		pw.CloseWithError(walkErr)
		errCh <- walkErr
	}()

	runErr := session.Run("mkdir -p /tmp/agentsdx-vm && tar -xzf - -C /tmp/agentsdx-vm")
	if runErr != nil {
		pr.CloseWithError(runErr) // unblock the goroutine
		<-errCh                   // wait for it to exit
		return fmt.Errorf("extract dir on remote: %w", runErr)
	}
	return <-errCh
}

// uploadFile uploads the contents of localPath to remotePath and marks it executable.
func uploadFile(conn sshConn, localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local file: %w", err)
	}
	session, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()
	session.Stdin = bytes.NewReader(data)
	return session.Run(fmt.Sprintf("cat > %s && chmod +x %s", remotePath, remotePath))
}

// runRemoteCommand runs cmd on the remote, streaming stdout/stderr to the local process.
func runRemoteCommand(conn sshConn, cmd string) error {
	session, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	return session.Run(cmd)
}
