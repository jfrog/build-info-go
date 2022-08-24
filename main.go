package main

import (
	"os"

	"github.com/jfrog/build-info-go/cli"
	"github.com/jfrog/build-info-go/utils"
	clitool "github.com/urfave/cli/v2"
)

const logLevelEnv = "BUILD_INFO_LOG_LEVEL"

var log utils.Log
var cliVersion = "dev"

func main() {
	log = utils.NewDefaultLogger(getCliLogLevel())
	app := &clitool.App{
		Name:     "Build-Info CLI",
		Usage:    "Generate build-info for your source code",
		Commands: cli.GetCommands(log),
		Version:  cliVersion,
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}
}

func getCliLogLevel() utils.LevelType {
	switch os.Getenv(logLevelEnv) {
	case "ERROR":
		return utils.ERROR
	case "WARN":
		return utils.WARN
	case "DEBUG":
		return utils.DEBUG
	default:
		return utils.INFO
	}
}
