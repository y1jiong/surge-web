package surge

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/adrg/xdg"
)

func getXDGBaseDir(envKey, fallback string) string {
	if dir := strings.TrimSpace(os.Getenv(envKey)); dir != "" {
		if filepath.IsAbs(dir) {
			return dir
		}
	}
	return fallback
}

func stateDir() string {
	if runtime.GOOS == "windows" {
		return surgeDir()
	}
	return filepath.Join(getXDGBaseDir("XDG_STATE_HOME", xdg.StateHome), "surge")
}

func surgeDir() string {
	if runtime.GOOS == "windows" {
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			if filepath.IsAbs(appData) {
				return filepath.Join(appData, "surge")
			}
		}
	}
	return filepath.Join(getXDGBaseDir("XDG_CONFIG_HOME", xdg.ConfigHome), "surge")
}

func runtimeDir() string {
	runtimeEnv := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	if runtimeEnv != "" && !filepath.IsAbs(runtimeEnv) {
		runtimeEnv = ""
	}

	runtimeBase := runtimeEnv
	if runtimeBase == "" {
		runtimeBase = strings.TrimSpace(xdg.RuntimeDir)
		if runtimeBase != "" && !filepath.IsAbs(runtimeBase) {
			runtimeBase = ""
		}
	}

	if (runtime.GOOS == "linux" || runtime.GOOS == "android") && runtimeEnv == "" {
		runtimeBase = ""
	}

	if runtimeBase == "" {
		return filepath.Join(stateDir(), "runtime")
	}
	return filepath.Join(runtimeBase, "surge")
}

func readTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	val := strings.TrimSpace(string(data))
	if val == "" {
		return "", fmt.Errorf("empty file: %s", path)
	}
	return val, nil
}

func DiscoverPort() (int, bool) {
	if portStr := strings.TrimSpace(os.Getenv("SURGE_PORT")); portStr != "" {
		var port int
		if _, err := fmt.Sscanf(portStr, "%d", &port); err == nil && port > 0 {
			return port, true
		}
	}
	portFile := filepath.Join(runtimeDir(), "port")
	if val, err := readTrimmed(portFile); err == nil {
		var port int
		if _, err := fmt.Sscanf(val, "%d", &port); err == nil && port > 0 {
			return port, true
		}
	}
	return 0, false
}

func DiscoverToken() (string, bool) {
	if t := strings.TrimSpace(os.Getenv("SURGE_TOKEN")); t != "" {
		return t, true
	}
	tokenFile := filepath.Join(stateDir(), "token")
	if val, err := readTrimmed(tokenFile); err == nil {
		return val, true
	}
	return "", false
}
