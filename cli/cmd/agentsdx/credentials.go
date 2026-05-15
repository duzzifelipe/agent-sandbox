package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/spf13/cobra"
)

func newCredentialsCmd(c *client.Client) *cobra.Command {
	parent := &cobra.Command{
		Use:   "credentials",
		Short: "Manage credentials stored in the vault",
	}
	parent.AddCommand(newCredentialsSetCmd(c))
	return parent
}

func newCredentialsSetCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "set <profile>",
		Short: "Copy local agent auth state into the profile vault",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := args[0]

			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("home dir: %w", err)
			}

			paths := []string{
				filepath.Join(home, ".claude"),
				filepath.Join(home, ".claude.json"),
			}

			tarball, err := createTarball(paths)
			if err != nil {
				return fmt.Errorf("build tarball: %w", err)
			}

			if err := c.SetCredentials(profile, tarball); err != nil {
				return fmt.Errorf("upload credentials: %w", err)
			}

			fmt.Printf("Credentials uploaded to profile %q.\n", profile)
			return nil
		},
	}
}

func createTarball(paths []string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for _, root := range paths {
		info, err := os.Lstat(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			if err := addDirToTar(tw, root, filepath.Base(root)); err != nil {
				return nil, err
			}
		} else {
			if err := addFileToTar(tw, root, filepath.Base(root)); err != nil {
				return nil, err
			}
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func addDirToTar(tw *tar.Writer, dirPath, baseName string) error {
	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(dirPath), path)
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
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
}

func addFileToTar(tw *tar.Writer, filePath, name string) error {
	info, err := os.Lstat(filePath)
	if err != nil {
		return err
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = name
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	return err
}
