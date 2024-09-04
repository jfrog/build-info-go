package dependencies

import (
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/jfrog/gofrog/crypto"
	gofrogcmd "github.com/jfrog/gofrog/io"

	"github.com/jfrog/build-info-go/build/utils/dotnet"
	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
)

const (
	PackagesFileName = "packages.config"
	utf16BOM         = "\uFEFF"
)

// Register packages.config extractor
func init() {
	register(&packagesExtractor{})
}

// packages.config dependency extractor
type packagesExtractor struct {
	allDependencies map[string]*buildinfo.Dependency
	childrenMap     map[string][]string
}

func (extractor *packagesExtractor) IsCompatible(projectName, dependenciesSource string, log utils.Log) bool {
	if strings.HasSuffix(dependenciesSource, PackagesFileName) {
		log.Debug("Found", dependenciesSource, "file for project:", projectName)
		return true
	}
	return false
}

func (extractor *packagesExtractor) DirectDependencies() ([]string, error) {
	return getDirectDependencies(extractor.allDependencies, extractor.childrenMap), nil
}

func (extractor *packagesExtractor) AllDependencies(log utils.Log) (map[string]*buildinfo.Dependency, error) {
	return extractor.allDependencies, nil
}

func (extractor *packagesExtractor) ChildrenMap() (map[string][]string, error) {
	return extractor.childrenMap, nil
}

// Create new packages.config extractor
func (extractor *packagesExtractor) new(dependenciesSource string, log utils.Log) (Extractor, error) {
	newExtractor := &packagesExtractor{allDependencies: map[string]*buildinfo.Dependency{}, childrenMap: map[string][]string{}}
	packagesConfig, err := newExtractor.loadPackagesConfig(dependenciesSource, log)
	if err != nil {
		return nil, err
	}

	globalPackagesCache, err := newExtractor.getGlobalPackagesCache()
	if err != nil {
		return nil, err
	}

	err = newExtractor.extract(packagesConfig, globalPackagesCache, log)
	return newExtractor, err
}

func (extractor *packagesExtractor) extract(packagesConfig *packagesConfig, globalPackagesCache string, log utils.Log) error {
	for _, nuget := range packagesConfig.XmlPackages {
		id := strings.ToLower(nuget.Id)
		nPackage := &nugetPackage{id: id, version: nuget.Version, dependencies: map[string]bool{}}
		// First lets check if the original version exists within the file system:
		pack, err := createNugetPackage(globalPackagesCache, nuget, nPackage, log)
		if err != nil {
			return err
		}
		if pack == nil {
			// If it doesn't exist lets build the array of alternative versions.
			alternativeVersions := CreateAlternativeVersionForms(nuget.Version)
			// Now let's do a loop to run over the alternative possibilities
			for i := 0; i < len(alternativeVersions); i++ {
				nPackage.version = alternativeVersions[i]
				pack, err = createNugetPackage(globalPackagesCache, nuget, nPackage, log)
				if err != nil {
					return err
				}
				if pack != nil {
					break
				}
			}
		}
		if pack != nil {
			extractor.allDependencies[id] = pack.dependency
			extractor.childrenMap[id] = pack.getDependencies()
		} else {
			log.Warn(fmt.Sprintf("The following NuGet package %s with version %s was not found in the NuGet cache %s."+absentNupkgWarnMsg, nuget.Id, nuget.Version, globalPackagesCache))
		}
	}
	return nil
}

// NuGet allows the version will be with missing or unnecessary zeros
// This method will return a list of the possible alternative versions
// "1.0" --> []string{"1.0.0.0", "1.0.0", "1"}
// "1" --> []string{"1.0.0.0", "1.0.0", "1.0"}
// "1.2" --> []string{"1.2.0.0", "1.2.0"}
// "1.22.33" --> []string{"1.22.33.0"}
// "1.22.33.44" --> []string{}
// "1.0.2" --> []string{"1.0.2.0"}
func CreateAlternativeVersionForms(originalVersion string) []string {
	versionSlice := strings.Split(originalVersion, ".")
	versionSliceSize := len(versionSlice)
	for i := 4; i > versionSliceSize; i-- {
		versionSlice = append(versionSlice, "0")
	}

	var alternativeVersions []string

	for i := 4; i > 0; i-- {
		version := strings.Join(versionSlice[:i], ".")
		if version != originalVersion {
			alternativeVersions = append(alternativeVersions, version)
		}
		if !strings.HasSuffix(version, ".0") {
			return alternativeVersions
		}
	}
	return alternativeVersions
}

func (extractor *packagesExtractor) loadPackagesConfig(dependenciesSource string, log utils.Log) (*packagesConfig, error) {
	content, err := os.ReadFile(dependenciesSource)
	if err != nil {
		return nil, err
	}

	config := &packagesConfig{}
	err = xmlUnmarshal(content, config, log)
	if err != nil {
		return nil, err
	}
	return config, nil
}

type dfsHelper struct {
	visited  bool
	notRoot  bool
	circular bool
}

func getDirectDependencies(allDependencies map[string]*buildinfo.Dependency, childrenMap map[string][]string) []string {
	helper := map[string]*dfsHelper{}
	for id := range allDependencies {
		helper[id] = &dfsHelper{}
	}

	for id := range allDependencies {
		if helper[id].visited {
			continue
		}
		searchRootDependencies(helper, id, allDependencies, childrenMap, map[string]bool{id: true})
	}
	var rootDependencies []string
	for id, nodeData := range helper {
		if !nodeData.notRoot || nodeData.circular {
			rootDependencies = append(rootDependencies, id)
		}
	}

	return rootDependencies
}

func searchRootDependencies(dfsHelper map[string]*dfsHelper, currentId string, allDependencies map[string]*buildinfo.Dependency, childrenMap map[string][]string, traversePath map[string]bool) {
	if dfsHelper[currentId].visited {
		return
	}
	for _, next := range childrenMap[currentId] {
		if _, ok := allDependencies[next]; !ok {
			// No such dependency
			continue
		}
		if traversePath[next] {
			for circular := range traversePath {
				dfsHelper[circular].circular = true
			}
			continue
		}

		// Not root dependency
		dfsHelper[next].notRoot = true
		traversePath[next] = true
		searchRootDependencies(dfsHelper, next, allDependencies, childrenMap, traversePath)
		delete(traversePath, next)
	}
	dfsHelper[currentId].visited = true
}

func createNugetPackage(packagesPath string, nuget xmlPackage, nPackage *nugetPackage, log utils.Log) (*nugetPackage, error) {
	nupkgPath := filepath.Join(packagesPath, nPackage.id, nPackage.version, strings.Join([]string{nPackage.id, nPackage.version, "nupkg"}, "."))

	exists, err := utils.IsFileExists(nupkgPath, false)

	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, nil
	}

	fileDetails, err := crypto.GetFileDetails(nupkgPath, true)
	if err != nil {
		return nil, err
	}
	nPackage.dependency = &buildinfo.Dependency{Id: nuget.Id + ":" + nuget.Version, Checksum: buildinfo.Checksum{Sha1: fileDetails.Checksum.Sha1, Md5: fileDetails.Checksum.Md5}}

	// Nuspec file that holds the metadata for the package.
	nuspecPath := filepath.Join(packagesPath, nPackage.id, nPackage.version, strings.Join([]string{nPackage.id, "nuspec"}, "."))
	nuspecContent, err := os.ReadFile(nuspecPath)
	if err != nil {
		return nil, err
	}
	nuspec := &nuspec{}
	err = xmlUnmarshal(nuspecContent, nuspec, log)
	if err != nil {
		pack := nPackage.id + ":" + nPackage.version
		log.Warn("Package:", pack, "couldn't be parsed due to:", err.Error(), ". Skipping the package dependency.")
		log.Debug("nuspec content:\n" + string(nuspecContent))
		return nPackage, nil
	}

	for _, dependency := range nuspec.Metadata.Dependencies.Dependencies {
		nPackage.dependencies[strings.ToLower(dependency.Id)] = true
	}

	for _, group := range nuspec.Metadata.Dependencies.Groups {
		for _, dependency := range group.Dependencies {
			nPackage.dependencies[strings.ToLower(dependency.Id)] = true
		}
	}

	return nPackage, nil
}

type nugetPackage struct {
	id           string
	version      string
	dependency   *buildinfo.Dependency
	dependencies map[string]bool // Set of dependencies
}

func (nugetPackage *nugetPackage) getDependencies() []string {
	var dependencies []string
	for key := range nugetPackage.dependencies {
		dependencies = append(dependencies, key)
	}

	return dependencies
}

func (extractor *packagesExtractor) getGlobalPackagesCache() (string, error) {
	localsCmd, err := dotnet.NewToolchainCmd(dotnet.Nuget)
	if err != nil {
		return "", err
	}
	// nuget locals global-packages -list
	localsCmd.Command = append(localsCmd.Command, []string{"locals", "global-packages"}...)
	localsCmd.CommandFlags = []string{"-list"}

	output, err := gofrogcmd.RunCmdOutput(localsCmd)
	if err != nil {
		return "", err
	}

	globalPackagesPath := strings.TrimSpace(strings.TrimPrefix(output, "global-packages:"))
	exists, err := utils.IsDirExists(globalPackagesPath, false)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("could not find global packages path at: %s", globalPackagesPath)
	}
	return globalPackagesPath, nil
}

// packages.config xml objects for unmarshalling
type packagesConfig struct {
	XMLName     xml.Name     `xml:"packages"`
	XmlPackages []xmlPackage `xml:"package"`
}

type xmlPackage struct {
	Id      string `xml:"id,attr"`
	Version string `xml:"version,attr"`
}

type nuspec struct {
	XMLName  xml.Name `xml:"package"`
	Metadata metadata `xml:"metadata"`
}

type metadata struct {
	Dependencies xmlDependencies `xml:"dependencies"`
}

type xmlDependencies struct {
	Groups       []group      `xml:"group"`
	Dependencies []xmlPackage `xml:"dependency"`
}

type group struct {
	TargetFramework string       `xml:"targetFramework,attr"`
	Dependencies    []xmlPackage `xml:"dependency"`
}

// xmlUnmarshal is a wrapper for xml.Unmarshal, handling wrongly encoded utf-16 XML by replacing "utf-16" with "utf-8" in the header.
func xmlUnmarshal(content []byte, obj interface{}, log utils.Log) (err error) {
	err = xml.Unmarshal(content, obj)
	if err != nil {
		log.Debug("Failed while trying to parse xml file. Nuspec file could be an utf-16 encoded file.\n" +
			"xml.Unmarshal doesn't support utf-16 encoding, so we need to decode the utf16 by ourselves.")

		buf := make([]uint16, len(content)/2)
		for i := 0; i < len(content); i += 2 {
			buf[i/2] = binary.LittleEndian.Uint16(content[i:])
		}
		// Remove utf-16 Byte Order Mark (BOM) if exists
		stringXml := strings.ReplaceAll(string(utf16.Decode(buf)), utf16BOM, "")

		// xml.Unmarshal doesn't support utf-16 encoding, so we need to convert the header to utf-8.
		stringXml = strings.Replace(stringXml, "utf-16", "utf-8", 1)

		err = xml.Unmarshal([]byte(stringXml), obj)
	}
	return
}
