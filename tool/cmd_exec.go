package tool

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os/exec"
)

func ExecCommand(strCommand string) (stdOut string, stdErr string, pid int, err error) {
	var res []byte
	var stderr bytes.Buffer
	cmd := exec.Command("/bin/bash", "-c", strCommand)
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println("Execute failed when Start:" + err.Error())
		return "", "", 0, err
	}
	defer stdout.Close()
	err = cmd.Start()
	if err != nil {
		return "", stderr.String(), 0, err
	}
	pid = cmd.Process.Pid
	res, err = ioutil.ReadAll(stdout)
	if err != nil {
		return "", stderr.String(), pid, err
	}
	err = cmd.Wait()
	return string(res), stderr.String(), pid, err
}
