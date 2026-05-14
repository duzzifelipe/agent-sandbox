package vm

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runVBoxManage executes VBoxManage with the given arguments and returns combined output.
func runVBoxManage(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "VBoxManage", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("VBoxManage %v: %w\noutput: %s", args, err, out)
	}
	return string(out), nil
}

// ParseVMInfo parses the --machinereadable output of VBoxManage showvminfo.
// Returns a map of key → value with surrounding quotes stripped.
func ParseVMInfo(output string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := line[:idx]
		val := strings.Trim(line[idx+1:], `"`)
		result[key] = val
	}
	return result
}

// ParseGuestProperty parses VBoxManage guestproperty get output.
// Returns the value and true if a value was found, or "", false if not set.
func ParseGuestProperty(output string) (string, bool) {
	output = strings.TrimSpace(output)
	if !strings.HasPrefix(output, "Value:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(output, "Value:")), true
}
