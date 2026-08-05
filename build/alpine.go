package build

import (
	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

const (
	AlpineScopeProd       = "prod"
	AlpineScopeTransitive = "transitive"
)

type AlpineModule struct {
	containingBuild *Build
	id              string
	repoKey         string
	alpineVersion   string
	preSnapshot     []buildutils.AlpinePackage
	cacheDir        string
	downloadsDir    string
	requestedPkgs   map[string]bool
}

func newAlpineModule(id, repoKey, alpineVersion string, containingBuild *Build) *AlpineModule {
	return &AlpineModule{
		containingBuild: containingBuild,
		id:              id,
		repoKey:         repoKey,
		alpineVersion:   alpineVersion,
		requestedPkgs:   make(map[string]bool),
	}
}

func (m *AlpineModule) SnapshotInstalledPackages() error {
	pkgs, err := buildutils.ListInstalledPackages()
	if err != nil {
		return err
	}
	m.preSnapshot = pkgs
	return nil
}

func (m *AlpineModule) SetPreSnapshot(snapshot []buildutils.AlpinePackage) {
	m.preSnapshot = snapshot
}

func (m *AlpineModule) SetCacheDir(dir string) {
	m.cacheDir = dir
}

func (m *AlpineModule) SetDownloadsDir(dir string) {
	m.downloadsDir = dir
}

func (m *AlpineModule) SetRequestedPackages(pkgNames []string) {
	m.requestedPkgs = make(map[string]bool, len(pkgNames))
	for _, name := range pkgNames {
		m.requestedPkgs[name] = true
	}
}

func (m *AlpineModule) CollectDependencies() ([]entities.Dependency, error) {
	afterSnapshot, err := buildutils.ListInstalledPackages()
	if err != nil {
		return nil, err
	}
	return m.collectDependenciesFromSnapshots(afterSnapshot), nil
}

func (m *AlpineModule) collectDependenciesFromSnapshots(afterSnapshot []buildutils.AlpinePackage) []entities.Dependency {
	added := buildutils.DiffAlpinePackages(m.preSnapshot, afterSnapshot)

	allByName := make(map[string]buildutils.AlpinePackage, len(afterSnapshot))
	for _, pkg := range afterSnapshot {
		allByName[pkg.Name] = pkg
	}

	providers := buildutils.BuildProviderIndex(afterSnapshot)
	depGraph := buildutils.BuildDepGraph(added, providers)

	addedByName := make(map[string]bool, len(added))
	for _, pkg := range added {
		addedByName[pkg.Name] = true
	}

	requestedByMap := buildRequestedBy(added, depGraph, m.requestedPkgs)

	seenDeps := make(map[string]struct{}, len(added))
	deps := make([]entities.Dependency, 0, len(added))

	for _, pkg := range added {
		if _, exists := seenDeps[pkg.ID()]; exists {
			continue
		}
		seenDeps[pkg.ID()] = struct{}{}

		scope := AlpineScopeTransitive
		if m.requestedPkgs[pkg.Name] {
			scope = AlpineScopeProd
		}
		dep := m.resolveDep(pkg, m.cacheDir, requestedByMap[pkg.Name])
		dep.Scopes = []string{scope}
		deps = append(deps, dep)
	}

	type preExisting struct {
		pkg         buildutils.AlpinePackage
		requestedBy [][]string
	}
	preExistingByID := make(map[string]*preExisting)
	var preExistingOrder []string

	for _, pkg := range added {
		for _, depToken := range pkg.Depends {
			depName := buildutils.ResolveDependencyProvider(depToken, providers)
			if depName == "" || depName == pkg.Name {
				continue
			}
			if addedByName[depName] {
				continue
			}
			depPkg, ok := allByName[depName]
			if !ok {
				continue
			}
			if _, alreadyRecorded := seenDeps[depPkg.ID()]; alreadyRecorded {
				continue
			}
			chains := parentRequestedByChains(pkg.ID(), requestedByMap[pkg.Name])
			if entry, seen := preExistingByID[depPkg.ID()]; seen {
				for _, chain := range chains {
					if len(entry.requestedBy) >= entities.RequestedByMaxLength {
						break
					}
					entry.requestedBy = append(entry.requestedBy, chain)
				}
			} else {
				if len(chains) > entities.RequestedByMaxLength {
					chains = chains[:entities.RequestedByMaxLength]
				}
				preExistingByID[depPkg.ID()] = &preExisting{
					pkg:         depPkg,
					requestedBy: chains,
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

		dep := m.resolveDep(entry.pkg, m.cacheDir, entry.requestedBy)
		dep.Scopes = []string{AlpineScopeTransitive}
		deps = append(deps, dep)
	}

	deps = append(deps, m.depsFromDownloadedArchives(seenDeps, requestedByMap)...)

	return deps
}

func (m *AlpineModule) depsFromDownloadedArchives(seenDeps map[string]struct{}, requestedByMap map[string][][]string) []entities.Dependency {
	if m.downloadsDir == "" {
		return nil
	}
	downloaded, err := buildutils.PackagesFromArchivesDir(m.downloadsDir)
	if err != nil {
		log.Warn("Could not list downloaded apk archives — Build Info may be missing dependencies: " + err.Error())
		return nil
	}

	var deps []entities.Dependency
	for _, pkg := range downloaded {
		if _, exists := seenDeps[pkg.ID()]; exists {
			continue
		}
		seenDeps[pkg.ID()] = struct{}{}

		scope := AlpineScopeTransitive
		if m.requestedPkgs[pkg.Name] {
			scope = AlpineScopeProd
		}
		dep := m.resolveDep(pkg, m.downloadsDir, requestedByMap[pkg.Name])
		dep.Scopes = []string{scope}
		deps = append(deps, dep)
		log.Debug("Recorded downloaded apk archive missing from the installed-package diff: " + pkg.ID())
	}
	return deps
}

func parentRequestedByChains(parentID string, parentRequestedBy [][]string) [][]string {
	if len(parentRequestedBy) == 0 {
		return [][]string{{parentID}}
	}
	chains := make([][]string, 0, len(parentRequestedBy))
	for _, path := range parentRequestedBy {
		chains = append(chains, append([]string{parentID}, path...))
	}
	return chains
}

func FlattenRequestedBy(chains [][]string) [][]string {
	if len(chains) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(chains))
	flat := make([]string, 0, len(chains))
	for _, chain := range chains {
		for _, id := range chain {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			flat = append(flat, id)
			if len(flat) == entities.RequestedByMaxLength {
				return [][]string{flat}
			}
		}
	}
	if len(flat) == 0 {
		return nil
	}
	return [][]string{flat}
}

func (m *AlpineModule) resolveDep(pkg buildutils.AlpinePackage, cacheDir string, requestedBy [][]string) entities.Dependency {
	var sha1Hex, sha256Hex, md5Hex string
	if cacheDir != "" {
		if checksums, err := buildutils.ChecksumsFromCache(pkg, cacheDir); err == nil {
			sha1Hex = checksums[crypto.SHA1]
			sha256Hex = checksums[crypto.SHA256]
			md5Hex = checksums[crypto.MD5]
		}
	}

	return entities.Dependency{
		Id: pkg.ID(),
		Checksum: entities.Checksum{
			Sha1:   sha1Hex,
			Sha256: sha256Hex,
			Md5:    md5Hex,
		},
		RequestedBy: FlattenRequestedBy(requestedBy),
	}
}

func (m *AlpineModule) CollectBuildInfo() error {
	deps, err := m.CollectDependencies()
	if err != nil {
		return err
	}
	return m.SaveBuildInfo(deps)
}

func (m *AlpineModule) SaveBuildInfo(deps []entities.Dependency) error {
	module := entities.Module{
		Id:           m.id,
		Type:         entities.Apk,
		Dependencies: deps,
	}
	buildInfo := &entities.BuildInfo{Modules: []entities.Module{module}}
	return m.containingBuild.SaveBuildInfo(buildInfo)
}

func buildRequestedBy(added []buildutils.AlpinePackage, depGraph map[string][]string, requested map[string]bool) map[string][][]string {
	idByName := make(map[string]string, len(added))
	depsMap := make(map[string]entities.Dependency, len(added))
	for _, pkg := range added {
		idByName[pkg.Name] = pkg.ID()
		depsMap[pkg.Name] = entities.Dependency{Id: pkg.ID()}
	}

	rootRequestedBy := [][]string{{}}
	for name := range requested {
		if _, ok := depsMap[name]; !ok {
			continue
		}
		populateAlpineRequestedBy(name, rootRequestedBy, depsMap, depGraph, idByName)
	}

	for _, pkg := range added {
		if requested[pkg.Name] || len(depsMap[pkg.Name].RequestedBy) > 0 {
			continue
		}
		populateAlpineRequestedBy(pkg.Name, rootRequestedBy, depsMap, depGraph, idByName)
	}

	result := make(map[string][][]string, len(added))
	for _, pkg := range added {
		if requested[pkg.Name] {
			result[pkg.Name] = nil
			continue
		}
		if chains := depsMap[pkg.Name].RequestedBy; len(chains) > 0 {
			result[pkg.Name] = chains
		}
	}
	return result
}

func populateAlpineRequestedBy(parentName string, parentRequestedBy [][]string, depsMap map[string]entities.Dependency, depGraph map[string][]string, idByName map[string]string) {
	parentID, ok := idByName[parentName]
	if !ok {
		return
	}
	for _, childName := range depGraph[parentName] {
		childDep, ok := depsMap[childName]
		if !ok {
			continue
		}
		if childDep.NodeHasLoop() || len(childDep.RequestedBy) >= entities.RequestedByMaxLength {
			continue
		}
		childDep.UpdateRequestedBy(parentID, parentRequestedBy)
		depsMap[childName] = childDep
		populateAlpineRequestedBy(childName, childDep.RequestedBy, depsMap, depGraph, idByName)
	}
}
