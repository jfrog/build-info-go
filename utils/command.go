package utils

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type Command struct {
	Executable string
	CmdName    string
	CmdArgs    []string
	Dir        string
	StrWriter  io.WriteCloser
	ErrWriter  io.WriteCloser
}

func NewCommand(executable, cmdName string, cmdArgs []string) *Command {
	return &Command{Executable: executable, CmdName: cmdName, CmdArgs: cmdArgs}
}

func (config *Command) RunWithOutput() (data []byte, err error) {
	cmd := config.GetCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("failed running command: '%s %s' with error: %s - %s",
			cmd.Path,
			strings.Join(cmd.Args, " "),
			err.Error(),
			stderr.String(),
		)
	}
	return stdout.Bytes(), nil
}

func (config *Command) GetCmd() (cmd *exec.Cmd) {
	var cmdStr []string
	if config.CmdName != "" {
		cmdStr = append(cmdStr, config.CmdName)
	}
	if config.CmdArgs != nil && len(config.CmdArgs) > 0 {
		cmdStr = append(cmdStr, config.CmdArgs...)
	}
	cmd = exec.Command(config.Executable, cmdStr...)
	cmd.Dir = config.Dir
	return
}

func (config *Command) GetEnv() map[string]string {
	return map[string]string{}
}

func (config *Command) GetStdWriter() io.WriteCloser {
	return config.StrWriter
}

func (config *Command) GetErrWriter() io.WriteCloser {
	return config.ErrWriter
}
