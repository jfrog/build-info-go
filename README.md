# build-info-go

## Overview

**build-info-go** is a CLI and a Go library used to collect build-info.

Read more about build-info and build integration in Artifactory [here](https://www.jfrog.com/confluence/display/JFROG/Build+Integration).

## Tests

To run the tests, execute the following command from within the root directory of the project:

```sh
go test -v ./...
```

## APIs

Collecting and building build-info for your project is easier than ever using the BuildInfoService:

### Creating a New Build

To use the APIs below, you need to create a new instance of BuildInfoService and then create a new Build (or get an existing one):

```go
service := build.NewBuildInfoService()
bld, err := service.GetOrCreateBuild(buildName, buildNumber)
```

It's important to invoke this function at the very beginning of the build, so that the start time property in the build-info will be accurate.

### Collecting Build-Info for Go Projects

After you [created a Build](#creating-a-new-build), you can create a new Go build-info module for your Go project and collect its dependencies:

```go
goModule, err := bld.AddGoModule(goProjectPath) // You can pass an empty string to find the Go project in the working directory
err = goModule.CalcDependencies()
```

You can also add artifacts to that module:

```go
artifact := entities.Artifact{Name: "v1.0.0.mod", Type: "mod", Checksum: &entities.Checksum{Sha1: "123", Md5: "456"}}
err = goModule.AddArtifacts(artifact)
```

### Get the Final BuildInfo

Using the `ToBuildInfo()` function you can create a final BuildInfo struct with all the information you collected:

```go
buildInfo, err := bld.ToBuildInfo()
```

### Clean the Build Cache

The Builds are saved in local cache, so you can create a Build and continue working on it at a later time.

If you finished working on a certain Build and you want to delete it from the cache, all you need to do is to call this function:

```go
err := bld.Clean()
```
