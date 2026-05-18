package nix

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseNarInfo parses a .narinfo file's plain-text content into a NixNarInfo struct.
// Format is line-based key-value: "Key: Value"
func ParseNarInfo(content string) (*NixNarInfo, error) {
	info := &NixNarInfo{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ": ")
		if idx == -1 {
			continue
		}
		key := line[:idx]
		value := line[idx+2:]

		switch key {
		case "StorePath":
			info.StorePath = value
		case "URL":
			info.URL = value
		case "Compression":
			info.Compression = value
		case "FileHash":
			info.FileHash = value
		case "FileSize":
			info.FileSize, _ = strconv.ParseInt(value, 10, 64)
		case "NarHash":
			info.NarHash = value
		case "NarSize":
			info.NarSize, _ = strconv.ParseInt(value, 10, 64)
		case "References":
			info.References = value
		case "Deriver":
			info.Deriver = value
		case "System":
			info.System = value
		case "Sig":
			info.Sig = value
		}
	}
	return info, nil
}

// ExtractStoreHash extracts the 32-char hash from a Nix store path.
// e.g. "/nix/store/yalw1pbrzmzk66phdkhslqh79pvbb67k-hello-2.12.3" → "yalw1pbrzmzk66phdkhslqh79pvbb67k"
func ExtractStoreHash(storePath string) string {
	base := filepath.Base(storePath)
	if idx := strings.Index(base, "-"); idx != -1 {
		return base[:idx]
	}
	return base
}

// ExtractPackageName extracts the human-readable name from a Nix store path.
// e.g. "/nix/store/yalw...-hello-2.12.3" → "hello-2.12.3"
func ExtractPackageName(storePath string) string {
	base := filepath.Base(storePath)
	if idx := strings.Index(base, "-"); idx != -1 {
		return base[idx+1:]
	}
	return base
}

// ExtractNameAndVersion splits a package name into name and version.
// e.g. "hello-2.12.3" → ("hello", "2.12.3")
// e.g. "glibc-2.40-66" → ("glibc", "2.40-66")
// e.g. "bash" → ("bash", "")
func ExtractNameAndVersion(packageName string) (string, string) {
	for i := len(packageName) - 1; i >= 0; i-- {
		if packageName[i] == '-' && i+1 < len(packageName) && packageName[i+1] >= '0' && packageName[i+1] <= '9' {
			return packageName[:i], packageName[i+1:]
		}
	}
	return packageName, ""
}

// StorePathToDepID converts a store path to a dependency ID in "name:version" format.
// e.g. "/nix/store/yalw...-hello-2.12.3" → "hello:2.12.3"
// If no version found: "bash" → "bash"
func StorePathToDepID(storePath string) string {
	pkgName := ExtractPackageName(storePath)
	name, version := ExtractNameAndVersion(pkgName)
	if version != "" {
		return name + ":" + version
	}
	return name
}

// SriToHex converts an SRI hash (sha256-<base64>=) to hex format.
// e.g. "sha256-lHk9nuLEIEnCaAvriEAfIbNxpQbopIyNmSU+YZcqZl0=" → "94793d9ee2c42049c2680beb88401f21b371a506e8a48c8d99253e61972a665d"
func SriToHex(sriHash string) (string, error) {
	if !strings.HasPrefix(sriHash, "sha256-") {
		return "", fmt.Errorf("unsupported SRI format: %s", sriHash)
	}
	b64 := strings.TrimPrefix(sriHash, "sha256-")
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	return fmt.Sprintf("%x", decoded), nil
}
