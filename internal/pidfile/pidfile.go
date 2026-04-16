package pidfile

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func Write(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0644)
}

func Read(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func IsRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func Remove(path string) {
	os.Remove(path)
}

func ReadPort(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	port := strings.TrimSpace(string(data))
	if port == "" {
		return "", fmt.Errorf("empty port file")
	}
	return port, nil
}

func WritePort(path string, port string) error {
	return os.WriteFile(path, []byte(port+"\n"), 0644)
}
