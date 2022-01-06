package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/jfrog/build-info-go/build"
	"github.com/jfrog/build-info-go/utils"
	"github.com/pkg/errors"
	clitool "github.com/urfave/cli/v2"
	"os"
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
				bld, err := service.GetOrCreateBuild("", "")
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
				bld, err := service.GetOrCreateBuild("", "")
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
				bld, err := service.GetOrCreateBuild("", "")
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
				bld, err := service.GetOrCreateBuild("", "")
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
		return errors.New(fmt.Sprintf("'%s' is not a valid value for '%s'", format, formatFlag))
	}

	return nil
}
