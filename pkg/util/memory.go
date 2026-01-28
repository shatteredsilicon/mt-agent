package util

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func GetMemoryTotal() (float64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}

		e := fmt.Errorf("invalid line in meminfo: %s", line)
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return 0, err
		}

		fv, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return 0, e
		}

		if len(parts) > 2 {
			fv *= 1024
		}

		return fv, nil
	}

	return 0, errors.New("can't get memory size from /proc/meminfo")
}
