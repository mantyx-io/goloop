package config

import (
	"os"
	"path/filepath"
)

const (
	localDirName   = ".goloop"
	localFileName  = "config.yaml"
	globalDirName  = ".goloop"
	globalFileName = "config.yaml"
)

// GlobalConfigPath returns ~/.goloop/config.yaml (override with GOOLOOP_GLOBAL_CONFIG).
func GlobalConfigPath() string {
	if v := os.Getenv("GOOLOOP_GLOBAL_CONFIG"); v != "" {
		return ExpandHome(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", globalDirName, globalFileName)
	}
	return filepath.Join(home, globalDirName, globalFileName)
}

// LocalConfigPath returns <project>/.goloop/config.yaml.
func LocalConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, localDirName, localFileName)
}

// ProjectGoloopDir returns <project>/.goloop.
func ProjectGoloopDir(projectRoot string) string {
	return filepath.Join(projectRoot, localDirName)
}

// IsInitialized reports whether the project has a goloop config (local or legacy).
func IsInitialized(projectRoot string) bool {
	if _, err := os.Stat(LocalConfigPath(projectRoot)); err == nil {
		return true
	}
	return findLegacyConfig(projectRoot) != ""
}

// GlobalConfigExists reports whether a global config file is present.
func GlobalConfigExists() bool {
	_, err := os.Stat(GlobalConfigPath())
	return err == nil
}

// Legacy config locations (project root); kept for backward compatibility.
func legacyConfigCandidates(projectRoot string) []string {
	return []string{
		filepath.Join(projectRoot, "goloop.yaml"),
		filepath.Join(projectRoot, "config", "goloop.yaml"),
	}
}

func findLegacyConfig(projectRoot string) string {
	for _, p := range legacyConfigCandidates(projectRoot) {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// ExpandHome expands a leading ~/ path.
func ExpandHome(path string) string {
	if len(path) < 2 || path[:2] != "~/" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
