package vm

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/duck-labs/agentsdx/internal/claudecreds"
	"github.com/duck-labs/agentsdx/internal/types"
)

func BuildUserData(authorizedKey, gitPrivateKey, profileName string, secrets map[string]string, projects []types.ProjectConfig) string {
	encodedKey := base64.StdEncoding.EncodeToString([]byte(gitPrivateKey))

	var extraEnv strings.Builder
	for k, v := range secrets {
		if k == claudecreds.VaultKey {
			continue
		}
		fmt.Fprintf(&extraEnv, "      %s=%s\n", k, v)
	}

	var cloneCmds strings.Builder
	for _, proj := range projects {
		cloneURL := proj.Repo
		if proj.AuthTokenEnv != "" {
			if token, ok := secrets[proj.AuthTokenEnv]; ok && token != "" {
				cloneURL = injectToken(proj.Repo, token)
			}
		}
		fmt.Fprintf(&cloneCmds, "  - su - ubuntu -c \"git clone %s %s\"\n", cloneURL, proj.Path)
	}

	var claudeWriteFiles strings.Builder
	var claudeRuncmds strings.Builder
	if rawCreds, ok := secrets[claudecreds.VaultKey]; ok && rawCreds != "" {
		encodedCreds := base64.StdEncoding.EncodeToString([]byte(rawCreds))
		claudeJSON := `{"hasCompletedOnboarding":true,"numStartups":1,"installMethod":"native","autoUpdates":false}`
		encodedClaudeJSON := base64.StdEncoding.EncodeToString([]byte(claudeJSON))

		fmt.Fprintf(&claudeWriteFiles, `  - path: /home/ubuntu/.claude/.credentials.json
    owner: 'ubuntu:ubuntu'
    permissions: '0600'
    encoding: b64
    content: %s
  - path: /home/ubuntu/.claude.json
    owner: 'ubuntu:ubuntu'
    permissions: '0644'
    encoding: b64
    content: %s
`, encodedCreds, encodedClaudeJSON)

		claudeRuncmds.WriteString("  - mkdir -p /home/ubuntu/.claude && chown -R ubuntu:ubuntu /home/ubuntu/.claude\n")
	}

	return fmt.Sprintf(`#cloud-config
ssh_authorized_keys:
  - %s
write_files:
  - path: /home/ubuntu/.ssh/id_rsa
    permissions: '0600'
    encoding: b64
    content: %s
  - path: /etc/agentsdx.env
    owner: 'ubuntu:ubuntu'
    permissions: '0600'
    content: |
      AGENTSDX_PROFILE=%s
%s%sruncmd:
  - mkdir -p /home/ubuntu/.ssh && chmod 700 /home/ubuntu/.ssh && chown -R ubuntu:ubuntu /home/ubuntu/.ssh
%s%s`, authorizedKey, encodedKey, profileName, extraEnv.String(), claudeWriteFiles.String(), claudeRuncmds.String(), cloneCmds.String())
}

func injectToken(repoURL, token string) string {
	u, err := url.Parse(repoURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return repoURL
	}
	u.User = url.User(token)
	return u.String()
}
