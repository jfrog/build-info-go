package build

import (
	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
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

// CollectDependencies diffs the post-command package list against the pre-snapshot, computes
// local checksums, assigns scopes and requestedBy chains, and returns the dep slice.
// Callers may enrich the returned slice (e.g. with AQL checksums) before calling SaveBuildInfo.
func (m *AlpineModule) CollectDependencies() ([]entities.Dependency, error) {
	afterSnapshot, err := buildutils.ListInstalledPackages()
	if err != nil {
		return nil, err
	}

	added := buildutils.DiffAlpinePackages(m.preSnapshot, afterSnapshot)

	// Build a forward dep graph (pkg.Name → []depName) to compute requestedBy chains.
	depGraph := buildutils.BuildDepGraph(added)

	// Build a name→AlpinePackage index for quick look-ups.
	pkgByName := make(map[string]buildutils.AlpinePackage, len(added))
	for _, pkg := range added {
		pkgByName[pkg.Name] = pkg
	}

	// Compute requestedBy for every newly added package.
	// - Explicitly requested → RequestedBy = [[]]     (direct from root)
	// - Transitive            → RequestedBy = [[<direct parent(s)>]]
	requestedByMap := buildRequestedBy(added, depGraph, m.requestedPkgs)

	seenDeps := make(map[string]struct{}, len(added))
	deps := make([]entities.Dependency, 0, len(added))
	for _, pkg := range added {
		if _, exists := seenDeps[pkg.ID()]; exists {
			continue
		}
		seenDeps[pkg.ID()] = struct{}{}

		// Primary: decode SHA1 directly from /lib/apk/db/installed (C: field).
		// This works even in Docker containers where the .apk cache is cleaned up.
		sha1Hex := pkg.SHA1Hex()

		// Fallback: compute checksums from the cached .apk archive (populated on
		// physical Alpine hosts or when the apk cache dir is explicitly mounted).
		var sha256Hex, md5Hex string
		if sha1Hex == "" {
			checksums, cacheErr := buildutils.ChecksumsFromCache(pkg, m.cacheDir)
			if cacheErr != nil {
				log.Debug("apk checksum cache miss for", pkg.ID(), ":", cacheErr)
			} else {
				log.Debug("apk checksum cache hit for", pkg.ID())
				sha1Hex = checksums[crypto.SHA1]
				sha256Hex = checksums[crypto.SHA256]
				md5Hex = checksums[crypto.MD5]
			}
		}

		scope := "transitive"
		if m.requestedPkgs[pkg.Name] {
			scope = "prod"
		}

		dep := entities.Dependency{
			Id: pkg.ID(),
			Checksum: entities.Checksum{
				Sha1:   sha1Hex,
				Sha256: sha256Hex,
				Md5:    md5Hex,
			},
			Scopes:      []string{scope},
			RequestedBy: requestedByMap[pkg.Name],
		}
		deps = append(deps, dep)
	}

	return deps, nil
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
	for _, pkg := range added {
		if requested[pkg.Name] {
			result[pkg.Name] = [][]string{{}} // root
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
