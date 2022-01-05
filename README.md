# build-info-go

## Project Status
[![Test](https://github.com/jfrog/build-info-go/actions/workflows/test.yml/badge.svg)](https://github.com/jfrog/build-info-go/actions/workflows/test.yml)

## Table of Contents

- [Overview](#overview)
- [Using build-info-go as a CLI](#using-build-info-go-as-a-cli)
  - [Build the CLI from Sources](#build-the-cli-from-sources)
  - [Generating Build-Info](#generating-build-info-using-the-cli)
  - [Logs](#logs)
- [Go APIs](#go-apis)
  - [Creating a New Build](#creating-a-new-build)
  - [Generating Build-Info](#generating-build-info)
  - [Collecting Environment Variables](#collecting-environment-variables)
  - [Get the Complete Build-Info](#get-the-complete-build-info)
  - [Clean the Build Cache](#clean-the-build-cache)
- [Tests](#tests)

## Overview

**build-info-go** is a Go library, which allows generating build-info for a source code project. The library is also packaged as a CLI.

Want to get additional enhancements when using build-info with the JFrog Platform? [Read more](https://www.jfrog.com/confluence/display/JFROG/Build+Integration) on how to do this in JFrog Artifactory.

## Using build-info-go as a CLI
### Download the CLI executable

|<img src="images/linux.png" valign="middle" width="20"> | Linux | [386](https://releases.jfrog.io/artifactory/bi-cli/v1/0.1.2/linux-386/bi) |[amd64](https://releases.jfrog.io/artifactory/bi-cli/v1/0.1.2/linux-amd64/bi) | [arm](https://releases.jfrog.io/artifactory/bi-cli/v1/0.1.2/linux-arm/bi) | [arm64](https://releases.jfrog.io/artifactory/bi-cli/v1/0.1.2/linux-arm64/bi) | [s390x](https://releases.jfrog.io/artifactory/bi-cli/v1/0.1.2/linux-s390x/bi) |  [ppc64](https://releases.jfrog.io/artifactory/bi-cli/v1/0.1.2/linux-ppc64/bi) | [ppc64le](https://releases.jfrog.io/artifactory/bi-cli/v1/0.1.2/linux-ppc64le/bi) |
| :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |

|<img src="images/mac.png" valign="middle" width="20"> | Mac | [386](https://releases.jfrog.io/artifactory/bi-cli/v1/0.1.2/mac-386/bi) | 
| :---: | :---: | :---: |

|<img src="images/windows.png" valign="middle" width="20"> | Windows | [amd64](https://releases.jfrog.io/artifactory/bi-cli/v1/0.1.2/windows-amd64/bi.exe) | 
| :---: | :---: | :---: |

### Build the CLI from Sources

Make sure Go is installed by running:
```
go version
```

Clone the sources and CD to the root directory of the project:
```
git clone https://github.com/jfrog/build-info-go
cd build-info-go
```
Build the sources as follows:

On Unix based systems run:
````
./buildscripts/build.sh
````
On Windows run:
````
.\buildscripts\build.bat
````
Once completed, you'll find the bi executable at the current directory.

### Generating Build-Info Using the CLI

The Build-Info CLI allows generating build-info for your project easily and quickly.

All you need to do is to navigate to the project's root directory and run one of the following commands (depending on the package manager you use). The complete build-info will be sent to the stdout.

#### Go

```shell
bi go
```
#### Maven

```shell
bi mvn
```
#### Gradle

```shell
bi gradle
```

### Logs

The default log level of the Build-Info CLI is INFO.

You can change the log level by setting the BUILD_INFO_LOG_LEVEL environment variable to either DEBUG, INFO, WARN or ERROR.

All log messages are sent to the stderr, to allow picking up the generated build-info, which is sent to the stdout.

## Go APIs

Collecting and building build-info for your project is easier than ever using the BuildInfoService:

### Creating a New Build

To use the APIs below, you need to create a new instance of BuildInfoService and then create a new Build (or get an existing one):

```go
service := build.NewBuildInfoService()
bld, err := service.GetOrCreateBuild(buildName, buildNumber)
```

It's important to invoke this function at the very beginning of the build, so that the start time property in the build-info will be accurate.

### Generating Build-Info

After you [created a Build](#creating-a-new-build), you can create a new build-info module for your specific project type and collect its dependencies:

#### Go
```go
// You can pass an empty string as an argument, if the root of the Go project is the working directory
goModule, err := bld.AddGoModule(goProjectPath)
// Calculate the dependencies used by this module, and store them in the module struct.
err = goModule.CalcDependencies()

// You can also add artifacts to that module:
artifact1 := entities.Artifact{Name: "v1.0.0.mod", Type: "mod", Checksum: &entities.Checksum{Sha1: "123", Md5: "456", Sha256: "789"}}
err = goModule.AddArtifacts(artifact1, artifact2, ...)

```

#### Maven
```go
// You can pass an empty string as an argument, if the root of the Maven project is the working directory
mavenModule, err := bld.AddMavenModule(mavenProjectPath)
// Calculate the dependencies used by this module, and store them in the module struct.
err = mavenModule.CalcDependencies()
```

#### Gradle
```go
// You can pass an empty string as an argument, if the root of the Gradle project is the working directory
gradleModule, err := bld.AddGradleModule(gradleProjectPath)
// Calculate the dependencies used by this module, and store them in the module struct.
err = gradleModule.CalcDependencies()
```

### Collecting Environment Variables

Using `CollectEnv()` you can collect environment variables and attach them to the build.

After calling `ToBuildInfo()` ([see below](#get-the-complete-build-info)), you can also filter the environment variables using the `IncludeEnv()` and `ExcludeEnv()` methods of BuildInfo.

```go
err := bld.CollectEnv()
buildInfo, err := bld.ToBuildInfo()
err = buildInfo.IncludeEnv("ENV_VAR", "JFROG_CLI_*")
err = buildInfo.ExcludeEnv("*password*", "*secret*", "*token*")
```

### Get the Complete Build-Info

Using the `ToBuildInfo()` method you can create a complete BuildInfo struct with all the information collected:

```go
buildInfo, err := bld.ToBuildInfo()
err = bld.Clean()
```

### Clean the Build Cache

The process of generating build-info uses the local file system as a caching layer. This allows using this library by multiple processes.

If you finished working on a certain Build and you want to delete it from the cache, all you need to do is to call this function:

```go
err := bld.Clean()
```

## Tests

To run the tests, execute the following command from within the root directory of the project:

```sh
go test -v ./...
```
