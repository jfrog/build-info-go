package build

import (
	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
)

// AlpineModule collects Build Info for Alpine package installations performed through jf apk.
type AlpineModule struct {
	containingBuild *Build
	id              string
	repoKey         string
	alpineVersion   string
	preSnapshot     []buildutils.AlpinePackage
	cacheDir        string
	// requestedPkgs is the set of package names the user explicitly asked to install
	// (i.e. the non-flag tokens passed to `apk add`). Packages not in this set that
	// appear in the post-snapshot diff are classified as transitive dependencies.
	requestedPkgs map[string]bool
}

// newAlpineModule creates an AlpineModule attached to the given Build.
func newAlpineModule(id, repoKey, alpineVersion string, containingBuild *Build) *AlpineModule {
	return &AlpineModule{
		containingBuild: containingBuild,
		id:              id,
		repoKey:         repoKey,
		alpineVersion:   alpineVersion,
		requestedPkgs:   make(map[string]bool),
	}
}

// SnapshotInstalledPackages records the currently installed packages so CollectBuildInfo can diff against them.
func (m *AlpineModule) SnapshotInstalledPackages() error {
	pkgs, err := buildutils.ListInstalledPackages()
	if err != nil {
		return err
	}
	m.preSnapshot = pkgs
	return nil
}

// SetPreSnapshot injects a pre-command package snapshot captured externally.
func (m *AlpineModule) SetPreSnapshot(snapshot []buildutils.AlpinePackage) {
	m.preSnapshot = snapshot
}

// SetCacheDir sets the directory where apk stored downloaded archives so checksums can be computed.
func (m *AlpineModule) SetCacheDir(dir string) {
	m.cacheDir = dir
}

// SetRequestedPackages records the package names the user explicitly asked to install.
// These receive scope "prod" and requestedBy=[[]]; all other new packages are "transitive".
func (m *AlpineModule) SetRequestedPackages(pkgNames []string) {
	m.requestedPkgs = make(map[string]bool, len(pkgNames))
	for _, name := range pkgNames {
		m.requestedPkgs[name] = true
	}
}

// CollectDependencies runs a two-phase collection:
//
// Phase 1 — newly installed packages (pre/post snapshot diff):
// Each package gets its checksum from the C: DB field, falling back to the .apk
// cache archive, and finally to hashing its installed files on disk.
//
// Phase 2 — pre-existing dependencies of newly installed packages:
// Packages that were already on the system before `apk add` but are listed as
// runtime dependencies (D: field) of newly installed packages are also recorded.
// Their checksums are resolved from the C: DB field and installed files on disk
// (no .apk cache is expected for pre-existing packages).
//
// Callers may further enrich the returned slice (e.g. with AQL checksums) before
// calling SaveBuildInfo.
func (m *AlpineModule) CollectDependencies() ([]entities.Dependency, error) {
	afterSnapshot, err := buildutils.ListInstalledPackages()
	if err != nil {
		return nil, err
	}

	added := buildutils.DiffAlpinePackages(m.preSnapshot, afterSnapshot)

	// Index every installed package by name for pre-existing dep lookup.
	allByName := make(map[string]buildutils.AlpinePackage, len(afterSnapshot))
	for _, pkg := range afterSnapshot {
		allByName[pkg.Name] = pkg
	}

	// Build forward dep graph (pkg.Name → []depName) for requestedBy chains.
	depGraph := buildutils.BuildDepGraph(added)

	// Track which packages were newly installed so we don't double-record them.
	addedByName := make(map[string]bool, len(added))
	for _, pkg := range added {
		addedByName[pkg.Name] = true
	}

	requestedByMap := buildRequestedBy(added, depGraph, m.requestedPkgs)

	seenDeps := make(map[string]struct{}, len(added))
	deps := make([]entities.Dependency, 0, len(added))

	// ── Phase 1: newly installed packages ────────────────────────────────────
	for _, pkg := range added {
		if _, exists := seenDeps[pkg.ID()]; exists {
			continue
		}
		seenDeps[pkg.ID()] = struct{}{}

		scope := "transitive"
		if m.requestedPkgs[pkg.Name] {
			scope = "prod"
		}
		dep := m.resolveDep(pkg, m.cacheDir, requestedByMap[pkg.Name])
		dep.Scopes = []string{scope}
		deps = append(deps, dep)
	}

	// ── Phase 2: pre-existing dependencies of newly installed packages ───────
	// Build a deduplicated list, tracking which parent packages requested each dep.
	type preExisting struct {
		pkg         buildutils.AlpinePackage
		requestedBy [][]string
	}
	preExistingByID := make(map[string]*preExisting)
	var preExistingOrder []string // preserve deterministic order

	for _, pkg := range added {
		for _, depName := range pkg.Depends {
			if addedByName[depName] {
				continue // already recorded in phase 1
			}
			depPkg, ok := allByName[depName]
			if !ok {
				continue // virtual or unresolvable dep
			}
			if _, alreadyRecorded := seenDeps[depPkg.ID()]; alreadyRecorded {
				continue
			}
			if entry, seen := preExistingByID[depPkg.ID()]; seen {
				if len(entry.requestedBy) < 15 {
					entry.requestedBy = append(entry.requestedBy, []string{pkg.ID()})
				}
			} else {
				preExistingByID[depPkg.ID()] = &preExisting{
					pkg:         depPkg,
					requestedBy: [][]string{{pkg.ID()}},
				}
				preExistingOrder = append(preExistingOrder, depPkg.ID())
			}
		}
	}

	for _, id := range preExistingOrder {
		entry := preExistingByID[id]
		if _, exists := seenDeps[entry.pkg.ID()]; exists {
			continue
		}
		seenDeps[entry.pkg.ID()] = struct{}{}

		dep := m.resolveDep(entry.pkg, "", entry.requestedBy)
		dep.Scopes = []string{"transitive"}
		deps = append(deps, dep)
	}

	return deps, nil
}

// resolveDep builds a Dependency for pkg, resolving checksums with a three-tier strategy:
//  1. C: field in /lib/apk/db/installed → SHA1
//  2. Cached .apk archive in cacheDir   → SHA1 + SHA256 + MD5
//  3. Installed files on disk           → SHA1 + SHA256 + MD5 (aggregated)
func (m *AlpineModule) resolveDep(pkg buildutils.AlpinePackage, cacheDir string, requestedBy [][]string) entities.Dependency {
	sha1Hex := pkg.SHA1Hex()
	var sha256Hex, md5Hex string

	// Tier 2: .apk archive checksum (available when apk cached the download).
	if sha1Hex == "" {
		if checksums, err := buildutils.ChecksumsFromCache(pkg, cacheDir); err == nil {
			sha1Hex = checksums[crypto.SHA1]
			sha256Hex = checksums[crypto.SHA256]
			md5Hex = checksums[crypto.MD5]
		}
	}

	// Tier 3: hash the package's installed files on disk.
	// Used when the .apk archive is gone (pre-existing packages, Docker layers, etc.)
	// and to fill in SHA256/MD5 when only SHA1 was available from the C: field.
	if sha256Hex == "" || md5Hex == "" {
		if fileChecksums, err := buildutils.ChecksumsFromInstalledFiles(pkg); err == nil && len(fileChecksums) > 0 {
			if sha1Hex == "" {
				sha1Hex = fileChecksums[crypto.SHA1]
			}
			if sha256Hex == "" {
				sha256Hex = fileChecksums[crypto.SHA256]
			}
			if md5Hex == "" {
				md5Hex = fileChecksums[crypto.MD5]
			}
		}
	}

	return entities.Dependency{
		Id: pkg.ID(),
		Checksum: entities.Checksum{
			Sha1:   sha1Hex,
			Sha256: sha256Hex,
			Md5:    md5Hex,
		},
		RequestedBy: requestedBy,
	}
}

// CollectBuildInfo diffs the post-command package list against the pre-snapshot, enriches
// checksums where possible, and saves the result as Build Info dependencies.
// For AQL-based checksum enrichment, use CollectDependencies() and SaveBuildInfo() separately.
func (m *AlpineModule) CollectBuildInfo() error {
	deps, err := m.CollectDependencies()
	if err != nil {
		return err
	}
	return m.SaveBuildInfo(deps)
}

// SaveBuildInfo persists a pre-collected dep slice as a Build Info module.
// This is the final step in the two-phase flow:
//
//	deps, _ := module.CollectDependencies()
//	// ... AQL enrichment of deps ...
//	module.SaveBuildInfo(deps)
func (m *AlpineModule) SaveBuildInfo(deps []entities.Dependency) error {
	module := entities.Module{
		Id:           m.id,
		Type:         entities.Alpine,
		Dependencies: deps,
	}
	buildInfo := &entities.BuildInfo{Modules: []entities.Module{module}}
	return m.containingBuild.SaveBuildInfo(buildInfo)
}

// buildRequestedBy constructs the requestedBy map (pkgName → [][]string) from the dependency
// graph and the set of explicitly requested packages.
//
// Explicitly requested packages have RequestedBy = [[]] (root level).
// Transitively installed packages have RequestedBy = [[<direct parent ID>]] for each parent
// in the newly-installed set that lists them as a dependency.
func buildRequestedBy(added []buildutils.AlpinePackage, depGraph map[string][]string, requested map[string]bool) map[string][][]string {
	// Build a name→ID map for added packages (so we can use full IDs in requestedBy chains).
	idByName := make(map[string]string, len(added))
	for _, pkg := range added {
		idByName[pkg.Name] = pkg.ID()
	}

	result := make(map[string][][]string, len(added))

	// Seed explicitly requested packages as root-level.
	// We use nil (not [][]string{{}}) so the requestedBy field is omitted from JSON
	// when there is no meaningful parent chain to show.
	for _, pkg := range added {
		if requested[pkg.Name] {
			result[pkg.Name] = nil
		}
	}

	// Walk the forward dep graph: for each parent→child edge where both parent and child
	// are in the newly-installed set, add the parent's ID to the child's requestedBy.
	for parentName, deps := range depGraph {
		parentID, parentAdded := idByName[parentName]
		if !parentAdded {
			continue
		}
		for _, depName := range deps {
			if _, childAdded := idByName[depName]; !childAdded {
				continue
			}
			// Guard against the RequestedByMaxLength (15) used in other PM integrations.
			if len(result[depName]) >= 15 {
				continue
			}
			result[depName] = append(result[depName], []string{parentID})
		}
	}

	return result
}
