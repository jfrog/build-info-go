package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/jfrog/build-info-go/build"
	"github.com/jfrog/build-info-go/utils"
	clitool "github.com/urfave/cli/v2"
)

func GetCommands(logger utils.Log) []*clitool.Command {
	return []*clitool.Command{
		{
			Name:      "go",
			Usage:     "collect build-info for a Go project",
			UsageText: "bi go",
			Action: func(context *clitool.Context) error {
				service := build.NewBuildInfoService()
				service.SetLogger(logger)
				bld, err := service.GetOrCreateBuild("", "")
				if err != nil {
					return err
				}
				defer bld.Clean()
				goModule, err := bld.AddGoModule("")
				if err != nil {
					return err
				}
				err = goModule.CalcDependencies()
				if err != nil {
					return err
				}
				buildInfo, err := bld.ToBuildInfo()
				if err != nil {
					return err
				}
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
				return nil
			},
		},
	}
}
