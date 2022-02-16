package utils

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func RunCommand(execPath string, cmdArgs []string) error {
	_, err := RunCommandWithOutput(execPath, cmdArgs)
	return err
}

func RunCommandWithOutput(execPath string, cmdArgs []string) (data []byte, err error) {
	cmd := exec.Command(execPath, cmdArgs...)
	cmd.Env = os.Environ()
	//	log.Debug(fmt.Sprintf("running command: %v", cmd.Args))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed running command: '%s %s' with error: %s - %s", execPath, strings.Join(cmdArgs, " "), err.Error(), stderr.String()))
	}
	return stdout.Bytes(), nil
}

func GetExecutablePath(executableName string) (executablePath string, err error) {
	executablePath, err = exec.LookPath(executableName)
	if err != nil {
		return
	}
	if executablePath == "" {
		return "", errors.New("Could not find the" + executableName + " executable in the system PATH")
	}

	return executablePath, nil
}

type Cmd struct {
	Executable string
	Command    []string
	Dir        string
	StrWriter  io.WriteCloser
	ErrWriter  io.WriteCloser
}

func NewCmd(executableName string, cmdArgs []string) (*Cmd, error) {
	execPath, err := GetExecutablePath(executableName)
	if err != nil {
		return nil, err
	}
	return &Cmd{Executable: execPath, Command: cmdArgs}, nil
}

func (config *Cmd) GetCmd() (cmd *exec.Cmd) {
	var cmdStr []string
	cmdStr = append(cmdStr, config.Executable)
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
