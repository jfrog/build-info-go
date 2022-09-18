# Release Notes

| Release notes moved to https://github.com/jfrog/build-info-go/releases |
|----------------------------------------------------------------------------------------------------------------------------------------------------------|

## 1.6.0 (September 18, 2022)
- Add support for the Poetry package manager

## 1.5.2 (September 13, 2022)
- Breaking change: Removed the redundant `SetArgsAndFlags` setter from `DotnetModule`.
- Add `DotnetModule` getters.

## 1.5.1 (September 5, 2022)
- Fix Nuget requestedBy calculation.

## 1.5.0 (Aug 28, 2022)
- Update dependencies
- Update go to 1.18
- Update jfrog-ecosystem-integration-env to latest
- Fix multi lines NuGet project definition

## 1.4.1 (July 3, 2022)
- Move IsStringInSlice out from fileutils
- The latest npm tests have been updated to work with npm versions 8.11.0 and above

## 1.4.0 (July 3, 2022)
- Changed RunCmdWithOutputParser to run in silent mode
- Changes for Yarn Audit

## 1.3.0 (June 7, 2022)
- Add Dotnet support
- Bug: Duplicate artifacts are counted by their paths instead of their names

## 1.2.6 (May 8, 2022)
- Added pip & pipenv usage to the README
- Bug fix - Missing npm dependencies cause an error

## 1.2.5 (April 21, 2022)
- Upgrade build-info-extractor-maven to 2.36.2
- Upgrade build-info-extractor-gradle to 4.28.2 
- Bug fix - Build-info collection for npm nay fail due to peer dependencies conflicts

## 1.2.4 (April 14, 2022)
- Bug fix - Missing npm depedencies should not return an error

## 1.2.3 (April 11, 2022)
- Support for Python

## 1.2.2 (March 31, 2022)
- Bug fix - Avoid adding optional npm dependencies to the build-info

## 1.2.1 (March 27, 2022)
- Upgrade build-info-extractor-maven to 2.36.1 and gradle-artifactory-plugin 4.28.1 

## 1.2.0 (March 24, 2022)
- Allow calculating npm deps without checksums
- Move build-info schema to a file and allow validating it

## 1.1.1 (March 18, 2022)
- Upgrade maven & gradle build-info extractors
- New static code analysis badges added to README

## 1.1.0 (February 24, 2022)
- Support for yarn
- Add checksum to npm dependencies
- Bug fix - Limit the total for RequestedBy, to avoid out-of-memory errors

## 1.0.1 (January 30, 2022)
- Bug fix - Gradle - Avoid potential ambigues task error
- Bug fix - Go - Change Go dependency ID syntax to '{dependencyName}:v{dependecyVersion)
- Bug fix - Add build name/number/project to Maven & Gradle extractors
- Bug fix - Avoid creating a redundant build-info module in some scenarios
- Bug fix - Implicit memory aliasing in for loop whereby the targetBuildInfo modules may be reused accidentally

## 1.0.0 (January 6, 2022)
- Support for generating build-info for npm
- Generate and populate sha256
- Populate requestedBy field
- Allow converting to CycloneDX BOM
- Upgrade build-info to 2.33.0 / 4.26.0

## 0.1.6 (December 31, 2021)
- Added isEqual func to module, artifact & dependency structs
- Allow ignoring go list errors

## 0.1.5 (December 13, 2021)
- Upgrade dependencies

## 0.1.4 (November 30, 2021)
- Upgrade dependencies

## 0.1.3 (November 29, 2021)
- Upgrade dependencies

## 0.1.2 (November 25, 2021)
- Bug fix - Publishing build-info can fail, if a previous build-info collection action left an empty cache file

## 0.1.1 (November 21, 2021)
- Hash build dir with sha256

## 0.1.0 (November 10, 2021)
- Initial release: support in generating build-info for Go, Maven and Gradle
