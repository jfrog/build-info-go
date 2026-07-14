package utils

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

const AlpineCacheDir = "/var/cache/apk"
const apkInstalledDB = "/lib/apk/db/installed"

// AlpinePackage holds the metadata needed to record a Build Info dependency.
type AlpinePackage struct {
	Name     string
	Version  string
	Arch     string
	Size     int    // installed size in bytes (I: field in /lib/apk/db/installed)
	Origin   string // origin package (o: field)
	URL      string // upstream source URL (U: field)
	Checksum string // raw C: field value, e.g. "Q1<base64-sha1>="
}

// SHA1Hex decodes the Alpine DB checksum field (C: field, format "Q1<base64>") into a
// standard lowercase hex SHA1 string. Returns "" if the checksum is absent or malformed.
func (p AlpinePackage) SHA1Hex() string {
	v := strings.TrimPrefix(p.Checksum, "Q1")
	if v == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(raw)
}

// ID returns the canonical Build Info dependency identifier in name:version format,
// consistent with the convention used by npm, pip, and other build-info-go modules.
func (p AlpinePackage) ID() string {
	return p.Name + ":" + p.Version
}

// ListInstalledPackages reads /lib/apk/db/installed directly (no subprocess) and returns
// all currently installed packages with full metadata. Returns an error if the database
// file cannot be read.
func ListInstalledPackages() ([]AlpinePackage, error) {
	pkgs, err := parseInstalledDB(apkInstalledDB)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", apkInstalledDB, err)
	}
	return pkgs, nil
}

// parseInstalledDB reads the APK installed-packages database file and returns deduplicated packages.
//
// The file format is a sequence of stanzas separated by blank lines. Each line is
// "<field-letter>:<value>". Relevant fields:
//
//	P — package name
//	V — version
//	A — architecture
//	I — installed size (bytes)
//	o — origin package
//	U — upstream URL
func parseInstalledDB(path string) ([]AlpinePackage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var packages []AlpinePackage
	seen := make(map[string]struct{})
	var cur AlpinePackage

	flush := func() {
		if cur.Name == "" {
			return
		}
		key := cur.ID()
		if _, dup := seen[key]; !dup {
			seen[key] = struct{}{}
			packages = append(packages, cur)
		}
		cur = AlpinePackage{}
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if len(line) < 2 || line[1] != ':' {
			continue
		}
		value := line[2:]
		switch line[0] {
		case 'P':
			cur.Name = value
		case 'V':
			cur.Version = value
		case 'A':
			cur.Arch = value
		case 'I':
			cur.Size, _ = strconv.Atoi(value)
		case 'o':
			cur.Origin = value
		case 'U':
			cur.URL = value
		case 'C':
			cur.Checksum = value
		}
	}
	flush()
	return packages, scanner.Err()
}


// ChecksumsFromCache locates the cached .apk archive in cacheDir and computes MD5, SHA1, and SHA256 checksums.
func ChecksumsFromCache(pkg AlpinePackage, cacheDir string) (map[crypto.Algorithm]string, error) {
	if cacheDir == "" {
		cacheDir = AlpineCacheDir
	}
	pattern := filepath.Join(cacheDir, fmt.Sprintf("%s-%s*.apk", pkg.Name, pkg.Version))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return map[crypto.Algorithm]string{}, nil
	}
	checksums, err := crypto.GetFileChecksums(matches[0])
	if err != nil {
		return nil, fmt.Errorf("failed to checksum cached apk %s: %w", matches[0], err)
	}
	return checksums, nil
}

// BuildDepGraph runs "apk info -r <name>" for each package and returns a forward dependency
// graph: pkg.Name → []dependency-name. This is used to compute requestedBy chains.
// Packages for which apk info is unavailable are simply omitted from the graph.
func BuildDepGraph(pkgs []AlpinePackage) map[string][]string {
	graph := make(map[string][]string, len(pkgs))
	for _, pkg := range pkgs {
		out, err := exec.Command("apk", "info", "--depends", pkg.Name).Output()
		if err != nil {
			log.Debug(fmt.Sprintf("apk info --depends %s: %v", pkg.Name, err))
			continue
		}
		deps := parseDependsOutput(string(out))
		if len(deps) > 0 {
			graph[pkg.Name] = deps
		}
	}
	return graph
}

// parseDependsOutput extracts bare package names from `apk info --depends <pkg>` output.
// The output format is:
//
//	<pkg>-<ver> depends on:
//	<dep1>
//	<dep2>
//	<blank line>
func parseDependsOutput(output string) []string {
	var deps []string
	inSection := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, " depends on:") {
			inSection = true
			continue
		}
		if inSection {
			if trimmed == "" {
				break
			}
			// Strip version constraints: "musl>=1.2.3" → "musl"
			name := trimmed
			for _, op := range []string{">=", "<=", "~=", "!=", ">", "<", "="} {
				if idx := strings.Index(name, op); idx != -1 {
					name = name[:idx]
					break
				}
			}
			// Strip "so:" provider prefixes used for shared-lib virtual packages
			name = strings.TrimPrefix(strings.TrimSpace(name), "so:")
			if name != "" {
				deps = append(deps, name)
			}
		}
	}
	return deps
}

// DiffAlpinePackages returns packages present in after but not in before, deduplicated by name+version.
func DiffAlpinePackages(before, after []AlpinePackage) []AlpinePackage {
	beforeSet := make(map[string]struct{}, len(before))
	for _, p := range before {
		beforeSet[p.Name+"-"+p.Version] = struct{}{}
	}

	seen := make(map[string]struct{})
	var added []AlpinePackage
	for _, p := range after {
		key := p.Name + "-" + p.Version
		if _, inBefore := beforeSet[key]; inBefore {
			continue
		}
		if _, alreadyAdded := seen[key]; alreadyAdded {
			continue
		}
		seen[key] = struct{}{}
		added = append(added, p)
	}
	return added
}
