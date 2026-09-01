// Package apt implements build-info collection for Debian/apt installations.
//
// Three-source pipeline (design doc §5):
//  1. apt-cache depends --recurse → dependency graph with relation labels
//  2. dpkg-query -W               → exact installed version + architecture
//  3. Packages indexes            → SHA256 / SHA1 / MD5 + pool path
//     /var/cache/apt/archives     → fallback: compute from a cached .deb
//
// The Packages indexes are located with `apt-get indextargets` and read through
// `apt-helper cat-file`, since apt stores them compressed (lz4) — see
// packagesIndexFiles / openPackagesIndex.
//
// No AQL or Artifactory API calls are needed; all checksums come from local sources.
package apt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// Scope constants per design doc §6 scope-mapping table.
const (
	scopeRequired    = "required"    // Depends, Pre-Depends
	scopeRecommended = "recommended" // Recommends
	scopeOptional    = "optional"    // Suggests
)

// aptEdge is a directed dep edge with its relation kind encoded as a scope string.
type aptEdge struct {
	child string
	scope string
}

// dpkgInfo holds the installed version and architecture for one package.
type dpkgInfo struct {
	version string
	arch    string
}

// AptConfig configures the Apt build-info collector.
type AptConfig struct {
	// AptCacheExecutable overrides the apt-cache binary. Default: "apt-cache".
	AptCacheExecutable string
	// DpkgQueryExecutable overrides the dpkg-query binary. Default: "dpkg-query".
	DpkgQueryExecutable string
	// AptGetExecutable overrides the apt-get binary, used to enumerate the
	// Packages index files via `apt-get indextargets`. Default: "apt-get".
	AptGetExecutable string
	// AptHelperPath overrides apt's private helper binary, used to read index
	// files regardless of their compression. Default: /usr/lib/apt/apt-helper.
	AptHelperPath string
	// ListsDir is the directory containing Packages index files. Only used as a
	// fallback when `apt-get indextargets` is unavailable.
	// Default: /var/lib/apt/lists
	ListsDir string
	// CacheDir is the directory containing cached .deb files.
	// Default: /var/cache/apt/archives
	CacheDir string
}

// PackageState is a snapshot of the installed package set, mapping package name
// to "version:architecture".
type PackageState map[string]string

// AptFlexPack collects Debian/apt build-info after an apt-get install completes.
type AptFlexPack struct {
	config AptConfig
	// rootPkgs are the packages the user explicitly requested.
	rootPkgs []string
	// edgeGraph: parent pkg name → []aptEdge (directed edges with scope labels).
	edgeGraph map[string][]aptEdge
	// allMembers: pkg name → dpkgInfo (version+arch from dpkg-query).
	allMembers map[string]*dpkgInfo
	// checksums: pkgID → Checksum  (pkgID = "name:version:arch").
	checksums map[string]entities.Checksum
	// baseline is the installed set captured immediately before the apt operation.
	// When set, only packages added or changed relative to it are reported as
	// dependencies — see CollectBuildInfo. Nil means report the whole closure.
	baseline PackageState
}

// NewAptFlexPack creates a new Apt collector with sane defaults.
func NewAptFlexPack(config AptConfig) *AptFlexPack {
	if config.AptCacheExecutable == "" {
		config.AptCacheExecutable = "apt-cache"
	}
	if config.DpkgQueryExecutable == "" {
		config.DpkgQueryExecutable = "dpkg-query"
	}
	if config.AptGetExecutable == "" {
		config.AptGetExecutable = "apt-get"
	}
	if config.AptHelperPath == "" {
		config.AptHelperPath = "/usr/lib/apt/apt-helper"
	}
	if config.ListsDir == "" {
		config.ListsDir = "/var/lib/apt/lists"
	}
	if config.CacheDir == "" {
		config.CacheDir = "/var/cache/apt/archives"
	}
	return &AptFlexPack{
		config:     config,
		edgeGraph:  make(map[string][]aptEdge),
		allMembers: make(map[string]*dpkgInfo),
		checksums:  make(map[string]entities.Checksum),
	}
}

// SnapshotInstalled captures the currently installed package set. Call it
// immediately before running the apt operation and hand the result to
// SetBaseline, so the collector can report only what the operation changed.
func (c *AptFlexPack) SnapshotInstalled() (PackageState, error) {
	out, err := exec.Command(c.config.DpkgQueryExecutable,
		"-W", "-f=${db:Status-Abbrev}\t${Package}\t${Version}\t${Architecture}\n").Output()
	if err != nil {
		// dpkg-query exits non-zero for reasons that still leave usable output;
		// only a genuine exec failure is fatal (see collectVersions).
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, err
		}
	}
	state := make(PackageState)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 4)
		if len(parts) != 4 {
			continue
		}
		// Status-Abbrev is like "ii " — only fully installed packages count as
		// present; "rc" (removed, config remaining) must not mask a later install.
		if !strings.HasPrefix(strings.TrimSpace(parts[0]), "ii") {
			continue
		}
		name, version, arch := strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3])
		if name != "" {
			state[name] = version + ":" + arch
		}
	}
	return state, nil
}

// SetBaseline records the pre-operation installed set. With a baseline present,
// CollectBuildInfo reports only packages the operation actually installed,
// upgraded or downgraded.
func (c *AptFlexPack) SetBaseline(s PackageState) {
	c.baseline = s
}

// changedFromBaseline reports whether the operation added or altered this package.
// With no baseline (nil map) every lookup returns "" and every installed package
// differs, so the whole closure qualifies — matching the previous behaviour.
func (c *AptFlexPack) changedFromBaseline(name string, info *dpkgInfo) bool {
	return c.baseline[name] != info.version+":"+info.arch
}

// CollectDependencies runs the three-source pipeline for rootPkgs.
// Call this after apt-get install has completed so dpkg-query finds the new packages.
func (c *AptFlexPack) CollectDependencies(rootPkgs []string) error {
	// Normalise once: apt accepts "curl=8.5.0" and "curl/noble" on the command
	// line, but apt-cache and dpkg-query only ever speak bare package names.
	c.rootPkgs = make([]string, 0, len(rootPkgs))
	for _, p := range rootPkgs {
		if n := packageNameOnly(p); n != "" {
			c.rootPkgs = append(c.rootPkgs, n)
		}
	}

	// Source 1: apt-cache depends --recurse → members + labelled edges
	if err := c.collectGraph(c.rootPkgs); err != nil {
		return fmt.Errorf("apt-cache depends: %w", err)
	}
	log.Debug(fmt.Sprintf("apt: graph collected — %d packages in closure", len(c.allMemberNames())))

	// Source 2: dpkg-query -W → exact installed version + arch
	if err := c.collectVersions(); err != nil {
		return fmt.Errorf("dpkg-query: %w", err)
	}
	log.Debug(fmt.Sprintf("apt: dpkg-query resolved %d installed packages", len(c.allMembers)))

	// Source 3: parse /var/lib/apt/lists Packages → SHA256/SHA1/MD5.
	// After apt-get update + install, the Packages index always covers every
	// installed package, so this resolves all checksums in the normal flow.
	if err := c.collectChecksumsFromLists(); err != nil {
		log.Warn("apt: Packages index parse failed: " + err.Error())
	}

	// Fallback: compute from cached .deb files in /var/cache/apt/archives for
	// any packages the Packages index missed (e.g., pre-installed from a removed repo).
	c.collectMissingChecksums()

	resolved, missing := 0, 0
	for name, info := range c.allMembers {
		if _, ok := c.checksums[pkgID(name, info.version, info.arch)]; ok {
			resolved++
		} else {
			missing++
		}
	}
	log.Debug(fmt.Sprintf("apt: checksums resolved=%d missing=%d", resolved, missing))

	return nil
}

// CollectBuildInfo assembles an entities.BuildInfo from the collected deps.
// moduleID is the build module identifier (e.g., the build name or --module value).
func (c *AptFlexPack) CollectBuildInfo(buildName, buildNumber, moduleID string) (*entities.BuildInfo, error) {
	if moduleID == "" {
		if len(c.rootPkgs) > 0 {
			moduleID = strings.Join(c.rootPkgs, "+")
		} else {
			moduleID = "apt-build"
		}
	}

	// Both the dependency map and the traversal graph are keyed by the full
	// dependency ID ("name:version:arch"), not the bare package name. They must
	// agree: entities.Dependency.NodeHasLoop compares Dependency.Id against the
	// RequestedBy entries, so keying the graph by bare name would silently
	// disable cycle detection — and Debian has genuine dependency cycles
	// (libc6 <-> libgcc-s1), which would then appear as packages listed in their
	// own ancestry.
	// Packages the user named explicitly are always reported, even when the
	// operation left them untouched. They are what the build declared it needs, so
	// omitting them would make build-info depend on whether the machine happened to
	// have them already — the same pipeline would emit different dependencies on a
	// warm host than on a clean one.
	rootNames := make(map[string]bool, len(c.rootPkgs))
	for _, rp := range c.rootPkgs {
		rootNames[packageNameOnly(rp)] = true
	}

	idOf := make(map[string]string, len(c.allMembers))
	// reportIDs marks what gets emitted: packages this operation installed,
	// upgraded or downgraded, plus the explicitly requested roots. The traversal
	// graph below is still built over the full closure so RequestedBy chains stay
	// accurate; only emission is filtered.
	reportIDs := make(map[string]bool, len(c.allMembers))
	for name, info := range c.allMembers {
		id := pkgID(name, info.version, info.arch)
		idOf[name] = id
		reportIDs[id] = rootNames[name] || c.changedFromBaseline(name, info)
	}

	depsMap := make(map[string]entities.Dependency, len(c.allMembers))
	stringGraph := make(map[string][]string, len(c.edgeGraph)+1)

	for name := range c.allMembers {
		id := idOf[name]
		dep := entities.Dependency{
			Id:   id,
			Type: "deb",
		}
		if cs, ok := c.checksums[id]; ok {
			dep.Checksum = cs
		}
		depsMap[id] = dep
	}

	// Single pass: build traversal graph and scope map together.
	// Only include edges whose child dpkg-query resolved.
	scopeMap := make(map[string]map[string]bool, len(c.allMembers))
	for parent, edges := range c.edgeGraph {
		parentID, ok := idOf[parent]
		if !ok {
			continue
		}
		for _, e := range edges {
			childID, ok := idOf[e.child]
			if !ok {
				continue
			}
			if !slices.Contains(stringGraph[parentID], childID) {
				stringGraph[parentID] = append(stringGraph[parentID], childID)
			}
			if scopeMap[childID] == nil {
				scopeMap[childID] = make(map[string]bool)
			}
			scopeMap[childID][e.scope] = true
		}
	}
	// Virtual root edges: module → explicitly requested packages.
	// Explicitly requested packages are 'required' unless an edge already scoped them.
	for _, rp := range c.rootPkgs {
		if rootID, ok := idOf[rp]; ok {
			if !slices.Contains(stringGraph[moduleID], rootID) {
				stringGraph[moduleID] = append(stringGraph[moduleID], rootID)
			}
			if scopeMap[rootID] == nil {
				scopeMap[rootID] = map[string]bool{scopeRequired: true}
			}
		}
	}

	// Populate RequestedBy from the module level (same pattern as Go collector).
	populateAptRequestedBy(moduleID, [][]string{{}}, depsMap, stringGraph, map[string]bool{moduleID: true})

	// Assemble the final dependency list, with deterministic scope ordering so
	// repeated runs over the same system produce identical build-info.
	//
	// Dependencies with no checksum are omitted. Artifactory matches build-info
	// dependencies to artifacts by checksum, so an entry without one can never
	// resolve — it renders as a missing artifact rather than conveying anything.
	//
	// This arises when a package's installed version appears in no configured
	// index and no cached .deb remains — typically a base-image package whose
	// version upstream has since superseded (e.g. libc6 2.39-0ubuntu8.7 installed
	// while the repositories carry 2.39-0ubuntu8). Such a version cannot be
	// recovered: fetching the version that *is* available would attach a different
	// build's hash to this package, which is worse than reporting none.
	deps := make([]entities.Dependency, 0, len(depsMap))
	skipped := 0
	var noChecksum []string
	for id, dep := range depsMap {
		if !reportIDs[id] {
			skipped++
			continue
		}
		if dep.Checksum.IsEmpty() {
			noChecksum = append(noChecksum, id)
			continue
		}
		scopes := make([]string, 0, len(scopeMap[id]))
		for scope := range scopeMap[id] {
			scopes = append(scopes, scope)
		}
		sort.Strings(scopes)
		dep.Scopes = scopes
		deps = append(deps, dep)
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].Id < deps[j].Id })
	if skipped > 0 {
		log.Info(fmt.Sprintf(
			"apt: recording %d package(s) installed by this build; %d already-present dependencies excluded",
			len(deps), skipped))
	}
	if len(noChecksum) > 0 {
		// Name them rather than just counting, so the omission stays diagnosable.
		sort.Strings(noChecksum)
		log.Debug(fmt.Sprintf("apt: omitted %d dependency/dependencies with no obtainable checksum "+
			"(installed version is absent from every configured package index and no cached .deb remains, "+
			"so it cannot be verified or resolved in Artifactory): %s",
			len(noChecksum), strings.Join(noChecksum, ", ")))
	}

	aptVer := getAptVersion(c.config.AptCacheExecutable)
	bi := &entities.BuildInfo{
		Name:    buildName,
		Number:  buildNumber,
		Started: time.Now().Format(entities.TimeFormat),
		Agent: &entities.Agent{
			Name:    "build-info-go",
			Version: "1.0.0",
		},
		BuildAgent: &entities.Agent{
			Name:    "apt",
			Version: aptVer,
		},
		Modules: []entities.Module{
			{
				Id:           moduleID,
				Type:         entities.Debian,
				Dependencies: deps,
			},
		},
	}

	log.Debug(fmt.Sprintf("apt: built BuildInfo with %d deps for module %s", len(deps), moduleID))
	return bi, nil
}

// --- internal helpers --------------------------------------------------------

// aptCacheClosureFlags bound `apt-cache depends --recurse` to the set of packages
// this install is actually responsible for.
//
//   - --installed stops the walk at packages that are not installed. Without it the
//     walk follows every alternative and virtual-package provider across the whole
//     archive: the closure of `curl` on a stock Ubuntu image is ~23,000 packages,
//     which after intersecting with the installed set still claims 166 of the
//     system's 169 packages as dependencies of curl.
//   - --no-suggests drops Suggests, which apt does not install. Suggests alone
//     accounted for 62 of curl's 97 remaining entries — packages that are present
//     for unrelated reasons and are in no sense dependencies of curl.
//
// Recommends are kept: apt installs them by default, so they genuinely form part
// of what the install pulled in (they are recorded under the "recommended" scope).
var aptCacheClosureFlags = []string{"--installed", "--no-suggests"}

func (c *AptFlexPack) collectGraph(rootPkgs []string) error {
	args := append([]string{"depends", "--recurse"}, aptCacheClosureFlags...)
	args = append(args, rootPkgs...)
	out, err := exec.Command(c.config.AptCacheExecutable, args...).Output()
	if err != nil {
		return err
	}
	graph, err := parseAptCacheDependsOutput(string(out))
	if err != nil {
		return err
	}
	c.edgeGraph = graph
	return nil
}

// aptRelationToScope maps apt-cache relation strings to build-info scope values.
// Returns ("", false) for relation kinds that are not dependencies (Conflicts, etc.).
func aptRelationToScope(relation string) (string, bool) {
	switch relation {
	case "Depends", "Pre-Depends":
		return scopeRequired, true
	case "Recommends":
		return scopeRecommended, true
	case "Suggests":
		return scopeOptional, true
	default:
		return "", false
	}
}

// parseAptCacheDependsOutput parses the text output of `apt-cache depends --recurse`.
//
// Output format:
//
//	curl                          ← package (no indent)
//	  Depends: libcurl4t64        ← relation: dep
//	  Recommends: ca-certificates
//	  |libcurl3-gnutls            ← alternative (| prefix)
//	  Depends: <libpkg-dev>       ← uninstalled virtual pkg (angle brackets)
//	libcurl4t64
//	  Depends: libc6
//	  ...
func parseAptCacheDependsOutput(output string) (map[string][]aptEdge, error) {
	graph := make(map[string][]aptEdge)
	var currentPkg string
	var currentRelation string

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if line[0] != ' ' && line[0] != '\t' {
			// Package name line
			pkg := stripAngleBrackets(strings.TrimSpace(line))
			if pkg != "" {
				currentPkg = pkg
				currentRelation = ""
				initGraphEntry(graph, currentPkg)
			}
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || currentPkg == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "|") {
			// Alternative dependency — same relation kind as the preceding line
			dep := stripAngleBrackets(strings.TrimSpace(trimmed[1:]))
			if dep != "" && currentRelation != "" {
				if scope, ok := aptRelationToScope(currentRelation); ok {
					graph[currentPkg] = append(graph[currentPkg], aptEdge{child: dep, scope: scope})
					initGraphEntry(graph, dep)
				}
			}
			continue
		}

		if idx := strings.IndexByte(trimmed, ':'); idx > 0 {
			relation := strings.TrimSpace(trimmed[:idx])
			dep := stripAngleBrackets(strings.TrimSpace(trimmed[idx+1:]))
			if dep != "" {
				currentRelation = relation
				if scope, ok := aptRelationToScope(relation); ok {
					graph[currentPkg] = append(graph[currentPkg], aptEdge{child: dep, scope: scope})
					initGraphEntry(graph, dep)
				}
			}
		}
	}
	return graph, nil
}

func initGraphEntry(g map[string][]aptEdge, pkg string) {
	if _, exists := g[pkg]; !exists {
		g[pkg] = nil
	}
}

func stripAngleBrackets(s string) string {
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	return strings.TrimSpace(s)
}

func (c *AptFlexPack) allMemberNames() []string {
	// All children are already keys in edgeGraph (initGraphEntry ensures this).
	names := make([]string, 0, len(c.edgeGraph))
	for n := range c.edgeGraph {
		names = append(names, n)
	}
	for _, rp := range c.rootPkgs {
		if _, exists := c.edgeGraph[rp]; !exists {
			names = append(names, rp)
		}
	}
	return names
}

func (c *AptFlexPack) collectVersions() error {
	if len(c.edgeGraph) == 0 && len(c.rootPkgs) == 0 {
		return nil
	}
	// Query the full installed set (no package-name args) to avoid ARG_MAX limits
	// on large closures. Filter to the closure in Go.
	out, err := exec.Command(c.config.DpkgQueryExecutable,
		"-W", "-f=${Package}\t${Version}\t${Architecture}\n").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return err
		}
	}
	closure := make(map[string]bool, len(c.edgeGraph)+len(c.rootPkgs))
	for n := range c.edgeGraph {
		closure[n] = true
	}
	for _, rp := range c.rootPkgs {
		closure[rp] = true
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 3)
		if len(parts) != 3 {
			continue
		}
		name, version, arch := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		if name != "" && version != "" && arch != "" && closure[name] {
			c.allMembers[name] = &dpkgInfo{version: version, arch: arch}
		}
	}
	return nil
}

func (c *AptFlexPack) collectChecksumsFromLists() error {
	paths := c.packagesIndexFiles()
	total := 0
	for _, path := range paths {
		n, err := c.parseOnePackagesFile(path)
		if err != nil {
			log.Debug(fmt.Sprintf("apt: skip Packages file %s: %v", path, err))
		}
		total += n
	}
	log.Debug(fmt.Sprintf("apt: resolved %d checksums from %d Packages index files", total, len(paths)))
	return nil
}

// packagesIndexFiles returns every Packages index file on disk.
//
// Two sources are combined:
//
//  1. `apt-get indextargets` — the supported API for listing indexes of
//     permanently-configured sources. It returns real on-disk filenames
//     regardless of compression format (lz4, xz, gz, plain).
//
//  2. A direct scan of ListsDir for any file whose name contains "_Packages"
//     — necessary for on-the-fly auth, where apt downloads Packages files
//     from a temporary sources.list that is not in the system configuration
//     and therefore invisible to `apt-get indextargets`. These files persist
//     in the lists dir after the temp source is removed.
//
// Results from both sources are deduped; both are read through apt-helper
// cat-file, which handles any compression format transparently.
func (c *AptFlexPack) packagesIndexFiles() []string {
	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	// Source 1: permanently-configured sources via indextargets.
	out, err := exec.Command(c.config.AptGetExecutable,
		"indextargets", "--format", "$(FILENAME)", "Created-By: Packages").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				add(line)
			}
		}
	} else {
		log.Debug(fmt.Sprintf("apt: 'apt-get indextargets' unavailable (%v)", err))
	}

	// Source 2: direct dir scan — catches Packages files from temp/removed sources.
	entries, scanErr := os.ReadDir(c.config.ListsDir)
	if scanErr == nil {
		for _, e := range entries {
			n := e.Name()
			// Match both uncompressed (_Packages) and compressed (_Packages.lz4 etc.)
			if strings.HasSuffix(n, "_Packages") || strings.Contains(n, "_Packages.") {
				add(filepath.Join(c.config.ListsDir, n))
			}
		}
	} else {
		// ReadDir unavailable — keep the original glob as last resort.
		if matches, _ := filepath.Glob(filepath.Join(c.config.ListsDir, "*_Packages")); len(matches) > 0 {
			for _, m := range matches {
				add(m)
			}
		}
	}

	return paths
}

func (c *AptFlexPack) parseOnePackagesFile(path string) (int, error) {
	r, err := c.openPackagesIndex(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = r.Close() }()
	return c.fillChecksumsFromReader(r)
}

// openPackagesIndex streams a Packages index, transparently decompressing it.
//
// apt's own `apt-helper cat-file` handles every format apt may have written
// (lz4, gz, xz, plain), which keeps this package free of any compression
// dependency. Falls back to a plain file read when the helper is missing.
func (c *AptFlexPack) openPackagesIndex(path string) (io.ReadCloser, error) {
	if _, err := os.Stat(c.config.AptHelperPath); err != nil {
		return os.Open(path)
	}
	cmd := exec.Command(c.config.AptHelperPath, "cat-file", path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &cmdReadCloser{ReadCloser: stdout, cmd: cmd}, nil
}

// cmdReadCloser reaps the helper process when the reader is closed, so a long
// collection run cannot accumulate zombies.
type cmdReadCloser struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (c *cmdReadCloser) Close() error {
	closeErr := c.ReadCloser.Close()
	// Kill the subprocess in case it blocked on a full pipe after we closed the
	// read end, then reap it. Ignore the resulting signal error from Kill.
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
	return closeErr
}

func (c *AptFlexPack) fillChecksumsFromReader(r io.Reader) (int, error) {
	count := 0
	for _, stanza := range parseDeb822(r) {
		name := stanza["Package"]
		version := stanza["Version"]
		arch := stanza["Architecture"]
		if name == "" || version == "" || arch == "" {
			continue
		}
		info, ok := c.allMembers[name]
		if !ok || info.version != version {
			continue
		}
		id := pkgID(name, version, arch)
		if _, alreadyDone := c.checksums[id]; alreadyDone {
			continue
		}
		sha256, sha1, md5 := stanza["SHA256"], stanza["SHA1"], stanza["MD5sum"]
		if sha256 != "" || sha1 != "" || md5 != "" {
			c.checksums[id] = entities.Checksum{Sha256: sha256, Sha1: sha1, Md5: md5}
			count++
		}
	}
	return count, nil
}

// collectMissingChecksums is a fallback that computes checksums from .deb files
// sitting in /var/cache/apt/archives for any packages the Packages index missed.
// In the normal jf-apt-install flow (apt-get update ran before install) this
// function is a no-op: the Packages index covers every installed package.
func (c *AptFlexPack) collectMissingChecksums() {
	// Index the cache dir once instead of globbing per package.
	entries, err := os.ReadDir(c.config.CacheDir)
	if err != nil {
		return
	}
	debsByName := make(map[string][]string, len(entries))
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".deb") {
			continue
		}
		// "name_version_arch.deb" — split on first '_'; epoch colons may be %3a-encoded
		if i := strings.IndexByte(n, '_'); i > 0 {
			debsByName[n[:i]] = append(debsByName[n[:i]], filepath.Join(c.config.CacheDir, n))
		}
	}
	for name, info := range c.allMembers {
		id := pkgID(name, info.version, info.arch)
		if _, ok := c.checksums[id]; ok {
			continue
		}
		for _, deb := range debsByName[name] {
			if cs, err := fileChecksums(deb); err == nil {
				c.checksums[id] = cs
				log.Debug(fmt.Sprintf("apt: fallback resolved %s from cached .deb %s", id, deb))
				break
			}
		}
	}
}

func fileChecksums(path string) (entities.Checksum, error) {
	details, err := crypto.GetFileDetails(path, true)
	if err != nil {
		return entities.Checksum{}, err
	}
	return entities.Checksum{
		Sha256: details.Checksum.Sha256,
		Sha1:   details.Checksum.Sha1,
		Md5:    details.Checksum.Md5,
	}, nil
}

// packageNameOnly strips apt's version and suite selectors from a requested
// package spec, so "curl=8.5.0-2ubuntu10" and "curl/noble" both resolve to
// "curl". apt accepts these forms on the command line (and --from-file allows
// name=version), but dpkg-query and apt-cache report the bare name.
func packageNameOnly(spec string) string {
	if i := strings.IndexAny(spec, "=/"); i != -1 {
		return spec[:i]
	}
	return spec
}

// pkgID builds the canonical "name:version:arch" dependency identifier.
func pkgID(name, version, arch string) string {
	return name + ":" + version + ":" + arch
}

// populateAptRequestedBy recursively fills RequestedBy fields (mirrors build/golang.go).
//
// Unlike the Go collector it carries an explicit onPath set of the current
// recursion stack. Debian graphs contain genuine cycles (libc6 <-> libgcc-s1,
// and libc6 sits in nearly every package's closure). The Go collector relies on
// entities.NodeHasLoop, which only trips *after* a cyclic path has been written
// — that both pollutes RequestedBy with packages listed inside their own
// ancestry and makes termination depend on that pollution. Skipping edges that
// close a cycle keeps the recursion finite and the output meaningful.
func populateAptRequestedBy(parentID string, parentRequestedBy [][]string,
	depsMap map[string]entities.Dependency, graph map[string][]string, onPath map[string]bool) {
	for _, childID := range graph[parentID] {
		if onPath[childID] {
			continue // edge closes a cycle
		}
		childDep, ok := depsMap[childID]
		if !ok {
			continue
		}
		if len(childDep.RequestedBy) >= entities.RequestedByMaxLength {
			continue
		}
		// The onPath check above only rejects edges closing a cycle on the current
		// stack. The parent's own RequestedBy may already carry the child from an
		// earlier branch, and UpdateRequestedBy would inherit those paths verbatim —
		// so drop them here too, otherwise the child still ends up in its own ancestry.
		acyclic := make([][]string, 0, len(parentRequestedBy))
		for _, path := range parentRequestedBy {
			if !slices.Contains(path, childID) {
				acyclic = append(acyclic, path)
			}
		}
		if len(acyclic) == 0 {
			continue
		}
		childDep.UpdateRequestedBy(parentID, acyclic)
		depsMap[childID] = childDep

		onPath[childID] = true
		populateAptRequestedBy(childID, childDep.RequestedBy, depsMap, graph, onPath)
		delete(onPath, childID)
	}
}

// ReadPackagesFile reads a package-list file (one package per line).
// Lines starting with '#' are comments; empty lines are ignored.
// Each non-comment line is a package name, optionally version-pinned: "curl=8.5.0".
func ReadPackagesFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkgs []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pkgs = append(pkgs, line)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found in %s", path)
	}
	return pkgs, nil
}

func getAptVersion(aptCacheExec string) string {
	out, err := exec.Command(aptCacheExec, "--version").Output()
	if err != nil {
		return "unknown"
	}
	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	if len(lines) > 0 {
		fields := strings.Fields(lines[0])
		if len(fields) >= 2 {
			return fields[1]
		}
	}
	return "unknown"
}
