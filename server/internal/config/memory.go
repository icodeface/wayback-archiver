package config

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// getTotalMemoryMB 获取系统总内存（MB），跨平台支持
func getTotalMemoryMB() int {
	switch runtime.GOOS {
	case "linux":
		return getLinuxMemoryMB()
	case "darwin":
		return getDarwinMemoryMB()
	case "windows":
		return getWindowsMemoryMB()
	default:
		// 未知平台，返回保守值
		return 512
	}
}

// getLinuxMemoryMB 从 /proc/meminfo 读取内存信息
func getLinuxMemoryMB() int {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 512 // fallback
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return int(kb / 1024) // KB -> MB
				}
			}
		}
	}
	return 512 // fallback
}

// getDarwinMemoryMB 使用 sysctl 获取 macOS 内存信息
func getDarwinMemoryMB() int {
	cmd := exec.Command("sysctl", "-n", "hw.memsize")
	output, err := cmd.Output()
	if err != nil {
		return 512 // fallback
	}

	bytes, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 512 // fallback
	}

	return int(bytes / 1024 / 1024) // bytes -> MB
}

// getWindowsMemoryMB 使用 wmic 获取 Windows 内存信息
func getWindowsMemoryMB() int {
	cmd := exec.Command("wmic", "ComputerSystem", "get", "TotalPhysicalMemory")
	output, err := cmd.Output()
	if err != nil {
		return 512 // fallback
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return 512 // fallback
	}

	bytes, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil {
		return 512 // fallback
	}

	return int(bytes / 1024 / 1024) // bytes -> MB
}
