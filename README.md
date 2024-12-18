<div align="center">

![Introduction gif](images/dark.gif#gh-dark-mode-only)
![Introduction gif](images/light.gif#gh-light-mode-only)

# Build Info Go

[![Scanned by Frogbot](https://raw.github.com/jfrog/frogbot/master/images/frogbot-badge.svg)](https://github.com/jfrog/frogbot#readme)
[![Go Report Card](https://goreportcard.com/badge/github.com/jfrog/build-info-go)](https://goreportcard.com/report/github.com/jfrog/build-info-go)
[![license](https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat)](./LICENSE) [![Website](https://img.shields.io/badge/buildinfo.org-%F0%9F%8C%8E-blue)](https://buildinfo.org)

</div>

| Branch |                                                                                                                                                                             Status                                                                                                                                                                             |
| :----: | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------: |
|  main  | [![Test](https://github.com/jfrog/build-info-go/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/jfrog/build-info-go/actions/workflows/test.yml) [![Static Analysis](https://github.com/jfrog/build-info-go/actions/workflows/analysis.yml/badge.svg?branch=main)](https://github.com/jfrog/build-info-go/actions/workflows/analysis.yml) |
|  dev   |  [![Test](https://github.com/jfrog/build-info-go/actions/workflows/test.yml/badge.svg?branch=dev)](https://github.com/jfrog/build-info-go/actions/workflows/test.yml) [![Static Analysis](https://github.com/jfrog/build-info-go/actions/workflows/analysis.yml/badge.svg?branch=dev)](https://github.com/jfrog/build-info-go/actions/workflows/analysis.yml)  |

## Table of Contents

- [Overview](#overview)
- [Schema](#schema)
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
Read more about build-info at [buildinfo.org](https://buildinfo.org/).

## Schema

The build-info schema is available [here](buildinfo-schema.json).

<details>
  <summary>Example</summary>
  
  ```json
    {
      "started": "2022-01-06T17:29:32.713+0200",
      "modules": [
        {
          "type": "go",
          "id": "github.com/jfrog/build-info-go",
          "dependencies": [
            {
              "id": "github.com/CycloneDX/cyclonedx-go:v0.4.0",
              "type": "zip",
              "sha1": "4fd24140c9d75be7361f204809ac509cfbec7d21",
              "md5": "2ac70bf2397c5bb980ccfd3dac6bd24d",
              "sha256": "329d65e011bde22c18a6210869b5ebe10cd943d53352b14d53d0b442007f279e"
            },
            {
              "id": "github.com/russross/blackfriday/v2:v2.0.1",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/cpuguy83/go-md2man/v2:v2.0.0-20190314233015-f79a8a8ca69d",
                  "github.com/urfave/cli/v2:v2.3.0",
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "afd8cfd78a268f5aaa7b86924145c333ea65c603",
              "md5": "8b04dcc4504ca8943c91a4b6cc59cda3",
              "sha256": "496079bbc8c4831cd0507213e059a925d2c22bd1ea9ada4dd85815d51b485228"
            },
            {
              "id": "github.com/buger/jsonparser:v1.1.1",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "e0c54d96564262a70bc7ed33fb3ee2b15596f68f",
              "md5": "7ab77d10951f73b96b9c19a6cca51bb1",
              "sha256": "be17ef1b44c22eac645eeac80f0e26cdfc70d77262e631358e00c2aa817eab8c"
            },
            {
              "id": "github.com/kr/pretty:v0.2.1",
              "type": "zip",
              "requestedBy": [
                [
                  "gopkg.in/check.v1:v1.0.0-20201130134442-10cb98267c6c",
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "e808602a157cdd88fc8984f27895fffd3d15ce8c",
              "md5": "353d5783d72d7e5b4409747b0be33177",
              "sha256": "80af0452082052d1b3265d7cb8985d464d4be222c27e14658e95632c222761e5"
            },
            {
              "id": "github.com/kr/text:v0.2.0",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "7d227e9c9516bd2a9617dfec9b150df1cc8d2ef3",
              "md5": "52630c25195715aa3b747ed34c8c1536",
              "sha256": "368eb318f91a5b67be905c47032ab5c31a1d49a97848b1011a0d0a2122b30ba4"
            },
            {
              "id": "github.com/pmezard/go-difflib:v1.0.0",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/stretchr/testify:v1.7.0",
                  "github.com/jfrog/gofrog:v1.1.1",
                  "github.com/jfrog/build-info-go"
                ],
                [
                  "github.com/stretchr/testify:v1.7.0",
                  "github.com/jfrog/gofrog:v1.1.1",
                  "github.com/jfrog/build-info-go"
                ],
                [
                  "github.com/stretchr/testify:v1.7.0",
                  "github.com/jfrog/build-info-go"
                ],
                [
                  "github.com/cpuguy83/go-md2man/v2:v2.0.0-20190314233015-f79a8a8ca69d",
                  "github.com/urfave/cli/v2:v2.3.0",
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "f200e2a5211b527ef2d2ff301718ccc4ad5c705b",
              "md5": "fb72df530a7f3fca56ccc192c9f30a58",
              "sha256": "de04cecc1a4b8d53e4357051026794bcbc54f2e6a260cfac508ce69d5d6457a0"
            },
            {
              "id": "github.com/stretchr/testify:v1.7.0",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/jfrog/gofrog:v1.1.1",
                  "github.com/jfrog/build-info-go"
                ],
                [
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "53b5c82ff76628b33b04017e8c81fbc1875f5737",
              "md5": "3cb74476ca750cb267db738a4db2f534",
              "sha256": "5a46ccebeff510df3e2f6d3842ee79d3f68d0e7b1554cd6ee93390d68b6c6b34"
            },
            {
              "id": "gopkg.in/check.v1:v1.0.0-20201130134442-10cb98267c6c",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "19bf400c2215e26dce7b3e966b0035d3c1dbdc87",
              "md5": "dcd82e15e290fa75348922f38492dae7",
              "sha256": "f555684e5c5dacc2850dddb345fef1b8f93f546b72685589789da6d2b062710e"
            },
            {
              "id": "github.com/jfrog/gofrog:v1.1.1",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "438ad3217d4ccbcb20bca8bfa5c1aa5aa704f9ed",
              "md5": "dc8cea2a1424c6abd4af2a74d2e680e2",
              "sha256": "137a603a124b5bfc14d13e17dbc8f50143aa64149cf0441b5ad10f59e08e72e4"
            },
            {
              "id": "github.com/minio/sha256-simd:v1.0.1-0.20210617151322-99e45fae3395",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "f091f68b7467e6dfb5ce28ae894b295525e59d47",
              "md5": "572ef4681740cfdacbbe601587609622",
              "sha256": "bb36b77f985b4ef963517202dbce3a9c72ffc7b90d70143ab4cd176981aa4c72"
            },
            {
              "id": "github.com/bradleyjkemp/cupaloy/v2:v2.6.0",
              "type": "zip",
              "sha1": "079e9f3594bab1a396ab9fe2d3fc5f5de1e7282a",
              "md5": "0aba1848e0f4de1bd5dcabd9569bf8f8",
              "sha256": "362b2b0446926332be700b60629d8788f622969d861fbcff7e65ccb97ed07fb3"
            },
            {
              "id": "github.com/pkg/errors:v0.8.0",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/jfrog/gofrog:v1.1.1",
                  "github.com/jfrog/build-info-go"
                ],
                [
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "f539bd34de2d4ab21c2865065eebc072c37c1194",
              "md5": "4030db591c8aca36aec6773ca552d95f",
              "sha256": "e4fa69ba057356614edbc1da881a7d3ebb688505be49f65965686bcb859e2fae"
            },
            {
              "id": "github.com/shurcoo!l/sanitized_anchor_name:v1.0.0",
              "type": "zip",
              "sha1": "fd4810a945b887a2e0f0ebb760131e13dca566ae",
              "md5": "90b29aa5c53c3df1b2b80e4d7220b1e3",
              "sha256": "0af034323e0627a9e94367f87aa50ce29e5b165d54c8da2926cbaffd5834f757"
            },
            {
              "id": "github.com/urfave/cli/v2:v2.3.0",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "0f882edb17acb1c544f6d53c5afa1d6d2add1308",
              "md5": "81a81c77ec9b2721e0229a66d5f77a83",
              "sha256": "bef25aedf2f3ac498094ec9cd216bca61ddf5f2eb7b1ecd850bbfb6053fe4103"
            },
            {
              "id": "gopkg.in/yaml.v3:v3.0.0-20200313102051-9f266ea9e77c",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/stretchr/testify:v1.7.0",
                  "github.com/jfrog/gofrog:v1.1.1",
                  "github.com/jfrog/build-info-go"
                ],
                [
                  "github.com/stretchr/testify:v1.7.0",
                  "github.com/jfrog/gofrog:v1.1.1",
                  "github.com/jfrog/build-info-go"
                ],
                [
                  "github.com/stretchr/testify:v1.7.0",
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "ec896ba2dc97dc3aa33066686b74259520428e00",
              "md5": "b8faa9934f8e54c43766ce7b4b2e0d49",
              "sha256": "acf19ccb4fca983b234a39ef032faf9ab70e759680673bb3dff077e77fee20fe"
            },
            {
              "id": "github.com/davecgh/go-spew:v1.1.1",
              "type": "zip",
              "sha1": "0f9760bda0c6ccacac5e57f62d0f5ad9c7dab03f",
              "md5": "feef6644bd69286382139b28be3f0b91",
              "sha256": "6b44a843951f371b7010c754ecc3cabefe815d5ced1c5b9409fb2d697e8a890d"
            },
            {
              "id": "github.com/klauspost/cpuid/v2:v2.0.6",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/minio/sha256-simd:v1.0.1-0.20210617151322-99e45fae3395",
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "1ed6884c9ee6ecf98727186591ec597771bd9abe",
              "md5": "e5a4769c581330d21ea90f433cec2ad0",
              "sha256": "514cbd03b0ded074640a9034af2cbc87490167a6d622a8c4bf478e153d8366e2"
            },
            {
              "id": "github.com/cpuguy83/go-md2man/v2:v2.0.0-20190314233015-f79a8a8ca69d",
              "type": "zip",
              "requestedBy": [
                [
                  "github.com/urfave/cli/v2:v2.3.0",
                  "github.com/jfrog/build-info-go"
                ]
              ],
              "sha1": "5586c962d5149ce9d73190ae61bab99ed56d4c7f",
              "md5": "ca2d6e511be9be839f06e049e710063e",
              "sha256": "38ea243c30ed1729d62ec8df91357ab040ac4967cc42d409b7600e0266f7e23c"
            }
          ]
        }
      ]
    }
  ```
</details>

## Using build-info-go as a CLI

### Download the CLI executable

| <img src="images/linux.png" valign="middle" width="20"> | Linux | [386](https://releases.jfrog.io/artifactory/bi-cli/v1/[RELEASE]/linux-386/bi) | [amd64](https://releases.jfrog.io/artifactory/bi-cli/v1/[RELEASE]/linux-amd64/bi) | [arm](https://releases.jfrog.io/artifactory/bi-cli/v1/[RELEASE]/linux-arm/bi) | [arm64](https://releases.jfrog.io/artifactory/bi-cli/v1/[RELEASE]/linux-arm64/bi) | [s390x](https://releases.jfrog.io/artifactory/bi-cli/v1/[RELEASE]/linux-s390x/bi) | [ppc64](https://releases.jfrog.io/artifactory/bi-cli/v1/[RELEASE]/linux-ppc64/bi) | [ppc64le](https://releases.jfrog.io/artifactory/bi-cli/v1/[RELEASE]/linux-ppc64le/bi) |
| :-----------------------------------------------------: | :---: | :---------------------------------------------------------------------------: | :-------------------------------------------------------------------------------: | :---------------------------------------------------------------------------: | :-------------------------------------------------------------------------------: | :-------------------------------------------------------------------------------: | :-------------------------------------------------------------------------------: | :-----------------------------------------------------------------------------------: |

| <img src="images/mac.png" valign="middle" width="20"> | Mac | [386](https://releases.jfrog.io/artifactory/bi-cli/v1/[RELEASE]/mac-386/bi) | [arm64](https://releases.jfrog.io/artifactory/bi-cli/v1/[RELEASE]/mac-arm64/bi) |
| :---------------------------------------------------: | :-: | :-------------------------------------------------------------------------: | ------------------------------------------------------------------------------- |

| <img src="images/windows.png" valign="middle" width="20"> | Windows | [amd64](https://releases.jfrog.io/artifactory/bi-cli/v1/[RELEASE]/windows-amd64/bi.exe) |
| :-------------------------------------------------------: | :-----: | :-------------------------------------------------------------------------------------: |

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

```
./buildscripts/build.sh
```

On Windows run:

```
.\buildscripts\build.bat
```

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

#### npm

```shell
bi npm [npm command] [command options]
```

Note: checksums calculation is not yet supported for npm projects.

#### Yarn

```shell
bi yarn [Yarn command] [command options]
```

Note: checksums calculation is not yet supported for Yarn projects.

#### pip

```shell
bi pip [pip command] [command options]
```

Note: checksums calculation is not yet supported for pip projects.

#### pipenv

```shell
bi pipenv [pipenv command] [command options]
```

Note: checksums calculation is not yet supported for pipenv projects.

#### twine

```shell
bi twine [twine command] [command options]
```

#### Dotnet

```shell
bi dotnet [Dotnet command] [command options]
```

#### Nuget

```shell
bi nuget [Nuget command] [command options]
```

#### Conversion to CycloneDX

You can generate build-info and have it converted into the CycloneDX format by adding to the
command `--format cyclonedx/xml` or `--format cyclonedx/json`.

### Logs

The default log level of the Build-Info CLI is INFO.

You can change the log level by setting the BUILD_INFO_LOG_LEVEL environment variable to either DEBUG, INFO, WARN or
ERROR.

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
// You can pass an empty string as an argument, if the root of the Go project is the working directory.
goModule, err := bld.AddGoModule(goProjectPath)
// Calculate the dependencies used by this module, and store them in the module struct.
err = goModule.CalcDependencies()

// You can also add artifacts to that module.
artifact1 := entities.Artifact{Name: "v1.0.0.mod", Type: "mod", Checksum: &entities.Checksum{Sha1: "123", Md5: "456", Sha256: "789"}}
err = goModule.AddArtifacts(artifact1, artifact2, ...)
```

#### Maven

```go
// You can pass an empty string as an argument, if the root of the Maven project is the working directory.
mavenModule, err := bld.AddMavenModule(mavenProjectPath)
// Calculate the dependencies used by this module, and store them in the module struct.
err = mavenModule.CalcDependencies()
```

#### Gradle

```go
// You can pass an empty string as an argument, if the root of the Gradle project is the working directory.
gradleModule, err := bld.AddGradleModule(gradleProjectPath)
// Calculate the dependencies used by this module, and store them in the module struct.
err = gradleModule.CalcDependencies()
```

#### npm

```go
// You can pass an empty string as an argument, if the root of the npm project is the working directory.
npmModule, err := bld.AddNpmModule(npmProjectPath)
// Checksum calculation is not supported for npm projects, so you can add a function that calculates them.
npmModule.SetTraverseDependenciesFunc(func (dependency *entities.Dependency) (bool, error) {
dependency.Checksum = &entities.Checksum{Sha1: "123"}
return true, nil
})
// Calculate the dependencies used by this module, and store them in the module struct.
err = npmModule.CalcDependencies()

// You can also add artifacts to that module.
artifact1 := entities.Artifact{Name: "json", Type: "tgz", Checksum: &entities.Checksum{Sha1: "123", Md5: "456"}}
err = npmModule.AddArtifacts(artifact1, artifact2, ...)
```

#### Yarn

```go
// You can pass an empty string as an argument, if the root of the Yarn project is the working directory.
yarnModule, err := bld.AddYarnModule(npmProjectPath)
// Checksum calculation is not supported for Yarn projects, so you can add a function that calculates them.
yarnModule.SetTraverseDependenciesFunc(func (dependency *entities.Dependency) (bool, error) {
dependency.Checksum = &entities.Checksum{Sha1: "123"}
return true, nil
})
// By default, your project will be built with the 'yarn install' command. If you want, you can set another command.
yarnModule.SetArgs([]string{"install", "--json"})
// Build the project, calculate the dependencies used by it and store them in the module struct.
err = yarnModule.Build()

// You can also add artifacts to that module.
artifact1 := entities.Artifact{Name: "json", Type: "tgz", Checksum: &entities.Checksum{Sha1: "123", Md5: "456"}}
err = yarnModule.AddArtifacts(artifact1, artifact2, ...)
```

#### Dotnet

```go
// You can pass an empty string as an argument, if the root of the Dotnet project is the working directory.
dotnetModule, err := bld.AddDotnetModules(nugetProjectPath)
// Calculate the dependencies used by this module, and store them in the module struct.
err = dotnetModule.CalcDependencies()
```

#### Nuget

```go
// You can pass an empty string as an argument, if the root of the Nuget project is the working directory.
nugetModule, err := bld.AddNugetModules(nugetProjectPath)
// Calculate the dependencies used by this module, and store them in the module struct.
err = nugetModule.CalcDependencies()
```

### Collecting Environment Variables

Using `CollectEnv()` you can collect environment variables and attach them to the build.

After calling `ToBuildInfo()` ([see below](#get-the-complete-build-info)), you can also filter the environment variables
using the `IncludeEnv()` and `ExcludeEnv()` methods of BuildInfo.

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
