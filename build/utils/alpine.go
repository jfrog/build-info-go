package utils

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

const apkInstalledDB = "/lib/apk/db/installed"

// apk writes cached archives as <name>-<version>.<8-hex-char-content-hash>.apk
// (e.g. curl-8.12.1-r0.1a7564c8.apk), not the bare <name>-<version>.apk the
// pattern originally assumed — that never matched a single real cache entry.
// The hash segment is made optional so both forms are recognized.
var apkArchiveNamePattern = regexp.MustCompile(`^(.+)-([^-]+-r\d+)(?:\.[0-9a-f]{8})?\.apk$`)

// AlpinePackage holds metadata for one installed or downloaded APK package.
type AlpinePackage struct {
	Name     string
	Version  string
	Arch     string
	Size     int
	Origin   string
	URL      string
	Depends  []string // D: tokens with so:/cmd:/pc: prefixes preserved
	Provides []string // p: tokens this package satisfies
	Files    []string
}

// ID returns the Build Info dependency id in "name:version" form.
func (p AlpinePackage) ID() string {
	return p.Name + ":" + p.Version
}

// ListInstalledPackages reads /lib/apk/db/installed and returns the installed packages.
func ListInstalledPackages() ([]AlpinePackage, error) {
	pkgs, err := parseInstalledDB(apkInstalledDB)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", apkInstalledDB, err)
	}
	return pkgs, nil
}

func parseInstalledDB(path string) ([]AlpinePackage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var packages []AlpinePackage
	seen := make(map[string]struct{})
	var cur AlpinePackage
	var currentDir string
	var stanzaStarted bool

	flush := func() {
		if cur.Name == "" {
			if stanzaStarted {
				log.Debug("Skipping incomplete APK installed DB stanza missing package name (P:)")
			}
			cur = AlpinePackage{}
			currentDir = ""
			stanzaStarted = false
			return
		}
		key := cur.ID()
		if _, dup := seen[key]; !dup {
			seen[key] = struct{}{}
			packages = append(packages, cur)
		}
		cur = AlpinePackage{}
		currentDir = ""
		stanzaStarted = false
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
		stanzaStarted = true
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
		case 'D':
			for _, spec := range strings.Fields(value) {
				if name := parseDependencySpec(spec); name != "" {
					cur.Depends = append(cur.Depends, name)
				}
			}
		case 'p':
			for _, spec := range strings.Fields(value) {
				if token := parseDependencySpec(spec); token != "" {
					cur.Provides = append(cur.Provides, token)
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
	return strings.TrimSpace(name)
}

// BuildProviderIndex maps package names and virtual provide tokens to the real package name that satisfies them.
func BuildProviderIndex(pkgs []AlpinePackage) map[string]string {
	index := make(map[string]string, len(pkgs)*2)
	for _, pkg := range pkgs {
		if pkg.Name != "" {
			index[pkg.Name] = pkg.Name
		}
	}
	for _, pkg := range pkgs {
		for _, token := range pkg.Provides {
			if _, taken := index[token]; !taken {
				index[token] = pkg.Name
			}
		}
	}
	return index
}

// ResolveDependencyProvider resolves a Depends/Provides token to the package name that provides it.
func ResolveDependencyProvider(depToken string, providers map[string]string) string {
	if depToken == "" {
		return ""
	}
	return providers[depToken]
}

// checksumGlobRetries and checksumGlobRetryDelay absorb the rare case where a package
// archive apk just wrote to cacheDir isn't visible yet to this glob on the first try.
const (
	checksumGlobRetries    = 3
	checksumGlobRetryDelay = 50 * time.Millisecond
)

// ChecksumsFromCache computes checksums for a package from a matching .apk archive under cacheDir.
// Returns an empty map when cacheDir is empty or no matching archive is found.
func ChecksumsFromCache(pkg AlpinePackage, cacheDir string) (map[crypto.Algorithm]string, error) {
	if cacheDir == "" {
		return map[crypto.Algorithm]string{}, nil
	}
	pattern := filepath.Join(cacheDir, fmt.Sprintf("%s-%s*.apk", pkg.Name, pkg.Version))
	var matches []string
	var err error
	for attempt := 1; attempt <= checksumGlobRetries; attempt++ {
		matches, err = filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		if len(matches) > 0 || attempt == checksumGlobRetries {
			break
		}
		time.Sleep(checksumGlobRetryDelay)
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

// PackagesFromArchivesDir parses package name/version pairs from *.apk filenames in dir.
func PackagesFromArchivesDir(dir string) ([]AlpinePackage, error) {
	if dir == "" {
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.apk"))
	if err != nil {
		return nil, err
	}
	pkgs := make([]AlpinePackage, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		groups := apkArchiveNamePattern.FindStringSubmatch(filepath.Base(match))
		if groups == nil {
			log.Debug("Ignoring unrecognized apk archive name:", filepath.Base(match))
			continue
		}
		pkg := AlpinePackage{Name: groups[1], Version: groups[2]}
		if _, dup := seen[pkg.ID()]; dup {
			continue
		}
		seen[pkg.ID()] = struct{}{}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

// BuildDepGraph builds a forward dependency graph of package name -> resolved child package names.
func BuildDepGraph(pkgs []AlpinePackage, providers map[string]string) map[string][]string {
	graph := make(map[string][]string, len(pkgs))
	for _, pkg := range pkgs {
		seen := make(map[string]struct{}, len(pkg.Depends))
		var children []string
		for _, depToken := range pkg.Depends {
			child := ResolveDependencyProvider(depToken, providers)
			if child == "" || child == pkg.Name {
				continue
			}
			if _, dup := seen[child]; dup {
				continue
			}
			seen[child] = struct{}{}
			children = append(children, child)
		}
		if len(children) > 0 {
			graph[pkg.Name] = children
		}
	}
	return graph
}

// DiffAlpinePackages returns packages present in after but not in before, keyed by name-version.
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
