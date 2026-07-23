package utils

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	Size     int      // installed size in bytes (I: field in /lib/apk/db/installed)
	Origin   string   // origin package (o: field)
	URL      string   // upstream source URL (U: field)
	Checksum string   // raw C: field value, e.g. "Q1<base64-sha1>="
	Depends  []string // runtime dependency names (D: field, version constraints stripped)
	Files    []string // absolute paths of all files installed by this package (F:+R: fields)
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
//	C — package checksum (Q1<base64-sha1>)
//	D — space-separated runtime dependencies (with optional version constraints)
//	F — directory path (sets context for subsequent R: lines)
//	R — file name within the current F: directory
func parseInstalledDB(path string) ([]AlpinePackage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var packages []AlpinePackage
	seen := make(map[string]struct{})
	var cur AlpinePackage
	var currentDir string // tracks current F: directory within a stanza

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
		currentDir = ""
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
		case 'D':
			for _, spec := range strings.Fields(value) {
				if name := parseDependencySpec(spec); name != "" {
					cur.Depends = append(cur.Depends, name)
				}
			}
		case 'F':
			currentDir = "/" + value
		case 'R':
			if currentDir != "" {
				cur.Files = append(cur.Files, currentDir+"/"+value)
			}
		}
	}
	flush()
	return packages, scanner.Err()
}

// parseDependencySpec strips version constraints and special prefixes from an APK
// dependency specification, returning the plain package name.
// Returns "" for conflict markers or specs that resolve to an empty name.
//
// Examples:
//
//	"musl>=1.2.3"       → "musl"
//	"so:libssl.so.3"    → "libssl.so.3"  (so: prefix stripped)
//	"!curl"             → ""             (conflict marker skipped)
func parseDependencySpec(spec string) string {
	if strings.HasPrefix(spec, "!") {
		return ""
	}
	name := spec
	for _, op := range []string{">=", "<=", "~=", "!=", ">", "<", "="} {
		if idx := strings.Index(name, op); idx != -1 {
			name = name[:idx]
			break
		}
	}
	name = strings.TrimPrefix(strings.TrimSpace(name), "so:")
	return name
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

// ChecksumsFromInstalledFiles computes aggregate checksums for a package by hashing every
// installed file that exists on the local filesystem (derived from F:/R: fields in the DB).
//
// Algorithm per hash type:
//  1. For each file in pkg.Files, check existence and compute SHA1/SHA256/MD5.
//  2. Sort the per-file hash strings for determinism.
//  3. Feed the sorted strings into the respective hasher to produce a single
//     package-level digest.
//
// Returns an empty map (no error) when pkg.Files is empty or no files are found on disk.
func ChecksumsFromInstalledFiles(pkg AlpinePackage) (map[crypto.Algorithm]string, error) {
	if len(pkg.Files) == 0 {
		return map[crypto.Algorithm]string{}, nil
	}

	type perFileHashes struct{ sha1, sha256, md5 string }
	var collected []perFileHashes

	for _, path := range pkg.Files {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		checksums, err := crypto.GetFileChecksums(path)
		if err != nil {
			log.Debug(fmt.Sprintf("ChecksumsFromInstalledFiles: error hashing %s: %v", path, err))
			continue
		}
		collected = append(collected, perFileHashes{
			sha1:   checksums[crypto.SHA1],
			sha256: checksums[crypto.SHA256],
			md5:    checksums[crypto.MD5],
		})
	}

	if len(collected) == 0 {
		return map[crypto.Algorithm]string{}, nil
	}

	// Sort by SHA256 for a deterministic order regardless of filesystem traversal order.
	sort.Slice(collected, func(i, j int) bool {
		return collected[i].sha256 < collected[j].sha256
	})

	// Aggregate each algorithm over the concatenation of the sorted per-file digests of
	// that same algorithm, reusing gofrog's CalcChecksums instead of hashing by hand.
	var sha1Buf, sha256Buf, md5Buf strings.Builder
	for _, h := range collected {
		sha1Buf.WriteString(h.sha1)
		sha256Buf.WriteString(h.sha256)
		md5Buf.WriteString(h.md5)
	}

	sha1Agg, err := crypto.CalcChecksums(strings.NewReader(sha1Buf.String()), crypto.SHA1)
	if err != nil {
		return nil, err
	}
	sha256Agg, err := crypto.CalcChecksums(strings.NewReader(sha256Buf.String()), crypto.SHA256)
	if err != nil {
		return nil, err
	}
	md5Agg, err := crypto.CalcChecksums(strings.NewReader(md5Buf.String()), crypto.MD5)
	if err != nil {
		return nil, err
	}

	return map[crypto.Algorithm]string{
		crypto.SHA1:   sha1Agg[crypto.SHA1],
		crypto.SHA256: sha256Agg[crypto.SHA256],
		crypto.MD5:    md5Agg[crypto.MD5],
	}, nil
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
