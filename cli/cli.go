package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/jfrog/build-info-go/build"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/build-info-go/utils/pythonutils"
	"github.com/pkg/errors"
	clitool "github.com/urfave/cli/v2"
	"os"
	"os/exec"
	"strings"
)

const (
	formatFlag    = "format"
	cycloneDxXml  = "cyclonedx/xml"
	cycloneDxJson = "cyclonedx/json"
)

func GetCommands(logger utils.Log) []*clitool.Command {
	flags := []clitool.Flag{
		&clitool.StringFlag{
			Name:  formatFlag,
			Usage: fmt.Sprintf("[Optional] Set to convert the build-info to a different format. Supported values are '%s' and '%s'.` `", cycloneDxXml, cycloneDxJson),
		},
	}

	return []*clitool.Command{
		{
			Name:      "go",
			Usage:     "Generate build-info for a Go project",
			UsageText: "bi go",
			Flags:     flags,
			Action: func(context *clitool.Context) (err error) {
				service := build.NewBuildInfoService()
				service.SetLogger(logger)
				bld, err := service.GetOrCreateBuild("go-build", "1")
				if err != nil {
					return
				}
				defer func() {
					e := bld.Clean()
					if err == nil {
						err = e
					}
				}()
				goModule, err := bld.AddGoModule("")
				if err != nil {
					return
				}
				err = goModule.CalcDependencies()
				if err != nil {
					return
				}
				return printBuild(bld, context.String(formatFlag))
			},
		},
		{
			Name:      "mvn",
			Usage:     "Generate build-info for a Maven project",
			UsageText: "bi mvn",
			Flags:     flags,
			Action: func(context *clitool.Context) (err error) {
				service := build.NewBuildInfoService()
				service.SetLogger(logger)
				bld, err := service.GetOrCreateBuild("mvn-build", "1")
				if err != nil {
					return
				}
				defer func() {
					e := bld.Clean()
					if err == nil {
						err = e
					}
				}()
				mavenModule, err := bld.AddMavenModule("")
				if err != nil {
					return
				}
				err = mavenModule.CalcDependencies()
				if err != nil {
					return
				}
				return printBuild(bld, context.String(formatFlag))
			},
		},
		{
			Name:      "gradle",
			Usage:     "Generate build-info for a Gradle project",
			UsageText: "bi gradle",
			Flags:     flags,
			Action: func(context *clitool.Context) (err error) {
				service := build.NewBuildInfoService()
				service.SetLogger(logger)
				bld, err := service.GetOrCreateBuild("gradle-build", "1")
				if err != nil {
					return
				}
				defer func() {
					e := bld.Clean()
					if err == nil {
						err = e
					}
				}()
				gradleModule, err := bld.AddGradleModule("")
				if err != nil {
					return
				}
				err = gradleModule.CalcDependencies()
				if err != nil {
					return
				}
				return printBuild(bld, context.String(formatFlag))
			},
		},
		{
			Name:      "npm",
			Usage:     "Generate build-info for an npm project",
			UsageText: "bi npm",
			Flags:     flags,
			Action: func(context *clitool.Context) (err error) {
				service := build.NewBuildInfoService()
				service.SetLogger(logger)
				bld, err := service.GetOrCreateBuild("npm-build", "1")
				if err != nil {
					return
				}
				defer func() {
					e := bld.Clean()
					if err == nil {
						err = e
					}
				}()
				npmModule, err := bld.AddNpmModule("")
				if err != nil {
					return
				}
				err = npmModule.CalcDependencies()
				if err != nil {
					return
				}
				return printBuild(bld, context.String(formatFlag))
			},
		},
		{
			Name:      "nuget",
			Usage:     "Generate build-info for a nuget project",
			UsageText: "bi nuget",
			Flags:     flags,
			Action: func(context *clitool.Context) (err error) {
				service := build.NewBuildInfoService()
				service.SetLogger(logger)
				bld, err := service.GetOrCreateBuild("nuget-build", "1")
				if err != nil {
					return
				}
				defer func() {
					e := bld.Clean()
					if err == nil {
						err = e
					}
				}()
				npmModule, err := bld.AddNpmModule("")
				if err != nil {
					return
				}
				err = npmModule.CalcDependencies()
				if err != nil {
					return
				}
				return printBuild(bld, context.String(formatFlag))
			},
		},
		{
			Name:            "yarn",
			Usage:           "Build a Yarn project and generate build-info for it",
			UsageText:       "bi yarn [yarn command] [command options]",
			Flags:           flags,
			SkipFlagParsing: true,
			Action: func(context *clitool.Context) (err error) {
				service := build.NewBuildInfoService()
				service.SetLogger(logger)
				bld, err := service.GetOrCreateBuild("yarn-build", "1")
				if err != nil {
					return
				}
				defer func() {
					e := bld.Clean()
					if err == nil {
						err = e
					}
				}()
				yarnModule, err := bld.AddYarnModule("")
				if err != nil {
					return
				}
				formatValue, filteredArgs, err := extractStringFlag(context.Args().Slice(), formatFlag)
				if err != nil {
					return
				}
				yarnModule.SetArgs(filteredArgs)
				err = yarnModule.Build()
				if err != nil {
					return
				}
				return printBuild(bld, formatValue)
			},
		},
		{
			Name:      "pip",
			Usage:     "Generate build-info for a pip project",
			UsageText: "bi pip",
			Flags:     flags,
			Action: func(context *clitool.Context) (err error) {
				service := build.NewBuildInfoService()
				service.SetLogger(logger)
				bld, err := service.GetOrCreateBuild("pip-build", "1")
				if err != nil {
					return
				}
				defer func() {
					e := bld.Clean()
					if err == nil {
						err = e
					}
				}()
				pythonModule, err := bld.AddPythonModule("", pythonutils.Pip)
				if err != nil {
					return
				}
				filteredArgs := filterCliFlags(context.Args().Slice(), flags)
				if filteredArgs[0] == "install" {
					err = pythonModule.RunInstallAndCollectDependencies(filteredArgs[1:])
					if err != nil {
						return
					}
					return printBuild(bld, context.String(formatFlag))
				} else {
					return exec.Command("pip", filteredArgs[1:]...).Run()
				}
			},
		},
		{
			Name:      "pipenv",
			Usage:     "Generate build-info for a pipenv project",
			UsageText: "bi pipenv",
			Flags:     flags,
			Action: func(context *clitool.Context) (err error) {
				service := build.NewBuildInfoService()
				service.SetLogger(logger)
				bld, err := service.GetOrCreateBuild("pipenv-build", "1")
				if err != nil {
					return
				}
				defer func() {
					e := bld.Clean()
					if err == nil {
						err = e
					}
				}()
				pythonModule, err := bld.AddPythonModule("", pythonutils.Pipenv)
				if err != nil {
					return
				}
				filteredArgs := filterCliFlags(context.Args().Slice(), flags)
				if filteredArgs[0] == "install" {
					err = pythonModule.RunInstallAndCollectDependencies(filteredArgs[1:])
					if err != nil {
						return
					}
					return printBuild(bld, context.String(formatFlag))
				} else {
					return exec.Command("pipenv", filteredArgs[1:]...).Run()
				}
			},
		},
	}
}

func printBuild(bld *build.Build, format string) error {
	buildInfo, err := bld.ToBuildInfo()
	if err != nil {
		return err
	}

	switch format {
	case cycloneDxXml:
		cdxBom, err := buildInfo.ToCycloneDxBom()
		if err != nil {
			return err
		}
		encoder := cdx.NewBOMEncoder(os.Stdout, cdx.BOMFileFormatXML)
		encoder.SetPretty(true)
		if err = encoder.Encode(cdxBom); err != nil {
			return err
		}
	case cycloneDxJson:
		cdxBom, err := buildInfo.ToCycloneDxBom()
		if err != nil {
			return err
		}
		encoder := cdx.NewBOMEncoder(os.Stdout, cdx.BOMFileFormatJSON)
		encoder.SetPretty(true)
		if err = encoder.Encode(cdxBom); err != nil {
			return err
		}
	case "":
		b, err := json.Marshal(buildInfo)
		if err != nil {
			return err
		}
		var content bytes.Buffer
		err = json.Indent(&content, b, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(content.String())
	default:
		return fmt.Errorf("'%s' is not a valid value for '%s'", format, formatFlag)
	}

	return nil
}

func extractStringFlag(args []string, flagName string) (flagValue string, filteredArgs []string, err error) {
	filteredArgs = []string{}
	for argIndex := 0; argIndex < len(args); argIndex++ {
		fullFlagName := "--" + flagName
		if args[argIndex] == fullFlagName {
			if len(args) <= argIndex+1 || strings.HasPrefix(args[argIndex+1], "-") {
				return "", nil, errors.New("Failed extracting value of provided flag: " + flagName)
			}
			flagValue = args[argIndex+1]
			argIndex++
		} else if argPrefix := fullFlagName + "="; strings.HasPrefix(args[argIndex], argPrefix) {
			flagValue = strings.TrimPrefix(args[argIndex], argPrefix)
		} else {
			filteredArgs = append(filteredArgs, args[argIndex])
		}
	}
	return
}

func filterCliFlags(allArgs []string, cliFlags []clitool.Flag) []string {
	var filteredArgs []string
	for _, arg := range allArgs {
		if !hasFlag(cliFlags, arg) {
			filteredArgs = append(filteredArgs, arg)
		}
	}
	return filteredArgs
}

func hasFlag(flagsList []clitool.Flag, arg string) bool {
	for _, flag := range flagsList {
		for _, name := range flag.Names() {
			if name == arg {
				return true
			}
		}
	}
	return false
}
