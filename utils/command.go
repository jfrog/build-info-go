package utils

import (
	"io"
	"os/exec"
)

type Cmd struct {
	ExecPath  string
	Command   []string
	Dir       string
	StrWriter io.WriteCloser
	ErrWriter io.WriteCloser
}

func NewCmd(executable string, cmdArgs []string) (*Cmd, error) {
	execPath, err := exec.LookPath(executable)
	if err != nil {
		return nil, err
	}
	return &Cmd{ExecPath: execPath, Command: cmdArgs}, nil
}

func (config *Cmd) GetCmd() (cmd *exec.Cmd) {
	var cmdStr []string
	cmdStr = append(cmdStr, config.ExecPath)
	cmdStr = append(cmdStr, config.Command...)
	cmd = exec.Command(cmdStr[0], cmdStr[1:]...)
	cmd.Dir = config.Dir
	return
}

func (config *Cmd) GetEnv() map[string]string {
	return map[string]string{}
}

func (config *Cmd) GetStdWriter() io.WriteCloser {
	return config.StrWriter
}

func (config *Cmd) GetErrWriter() io.WriteCloser {
	return config.ErrWriter
}
