#!/bin/bash
set -eu

#function build(pkg, goos, goarch, exeName)
build () {
  pkg="$1"
  export GOOS="$2"
  export GOARCH="$3"
  exeName="$4"
  echo "Building $exeName for $GOOS-$GOARCH ..."

  CGO_ENABLED=0 go build -o "$exeName" -ldflags '-w -extldflags "-static"' main.go
}

#function buildAndUpload(pkg, goos, goarch, fileExtension)
buildAndUpload () {
  pkg="$1"
  goos="$2"
  goarch="$3"
  fileExtension="$4"
  exeName="bi$fileExtension"

  build $pkg $goos $goarch $exeName

  destPath="$pkgPath/$version/$pkg/$exeName"
  echo "Uploading $exeName to $destPath ..."

  ./jfrog rt u "./$exeName" "$destPath"
  exitCode=$?

}

#function copyToLatestDir()
copyToLatestDir () {
  echo "Copy version to latest dir: $pkgPath/$version/"

  ./jfrog rt cp "$pkgPath/$version/(*)" "$pkgPath/latest/{1}" --flat
  exitCode=$?
}

verifyVersionMatching () {
  echo "Verifying provided version matches built version..."
  go build -o bi
  res=$(eval "./bi -v")
  exitCode=$?
  if [[ $exitCode -ne 0 ]]; then
    echo "Error: Failed verifying version matches"
    exit $exitCode
  fi

  # Get the version which is after the last space. (expected output to -v for example: "plugin-name version v1.0.0")
  echo "Output: $res"
  builtVersion="${res##* }"
  # Compare versions
  if [[ "$builtVersion" != "$version" ]]; then
    echo "Versions dont match. Provided: $version, Actual: $builtVersion"
    exit 1
  fi
  echo "Versions match."
}

version="$1"
pkgPath="ecosys-bi-cli/v1"

# Verify version provided in pipelines UI matches version in build-info-go source code.
verifyVersionMatching

# Build and upload for every architecture.
# Keep 'linux-386' first to prevent unnecessary uploads in case the built version doesn't match the provided one.
buildAndUpload 'linux-386' 'linux' '386' ''
buildAndUpload 'linux-amd64' 'linux' 'amd64' ''
buildAndUpload 'linux-s390x' 'linux' 's390x' ''
buildAndUpload 'linux-arm64' 'linux' 'arm64' ''
buildAndUpload 'linux-arm' 'linux' 'arm' ''
buildAndUpload 'mac-386' 'darwin' 'amd64' ''
buildAndUpload 'windows-amd64' 'windows' 'amd64' '.exe'

# Copy the uploaded version to override latest dir
copyToLatestDir