//go:build !windows
// +build !windows
package security

import (
	"os"
	"strings"
)

func IsBeingDebugged() bool {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "TracerPid:") && !strings.HasSuffix(line, "0") {
			return true
		}
	}
	return false
}
