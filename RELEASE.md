# Release Notes

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
