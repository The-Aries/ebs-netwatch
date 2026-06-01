package network

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Info struct {
	DefaultInterface string `json:"defaultInterface"`
	ConnectionType   string `json:"connectionType"`
	LinkSpeedMbps    int    `json:"linkSpeedMbps,omitempty"`
	Gateway          string `json:"gateway,omitempty"`
}

func DetectDefaultRoute() (string, string, error) {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return "", "", fmt.Errorf("open /proc/net/route: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", "", err
		}
		return "", "", fmt.Errorf("default route not found")
	}

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		iface := fields[0]
		destination := fields[1]
		gatewayHex := fields[2]
		if destination != "00000000" {
			continue
		}
		gateway, err := decodeIPv4Hex(gatewayHex)
		if err != nil {
			return "", "", err
		}
		return iface, gateway, nil
	}
	if err := scanner.Err(); err != nil {
		return "", "", err
	}
	return "", "", fmt.Errorf("default route not found")
}

func ConnectionType(iface string) string {
	if strings.TrimSpace(iface) == "" {
		return "unknown"
	}
	wirelessPath := filepath.Join("/sys/class/net", iface, "wireless")
	if _, err := os.Stat(wirelessPath); err == nil {
		return "wifi"
	}
	if strings.HasPrefix(iface, "wl") || strings.HasPrefix(iface, "wlan") {
		return "wifi"
	}
	if _, err := os.Stat(filepath.Join("/sys/class/net", iface)); err == nil && iface != "lo" {
		return "ethernet"
	}
	return "unknown"
}

func LinkSpeedMbps(iface string) (int, error) {
	if strings.TrimSpace(iface) == "" {
		return 0, fmt.Errorf("missing interface name")
	}
	raw, err := os.ReadFile(filepath.Join("/sys/class/net", iface, "speed"))
	if err != nil {
		return 0, err
	}
	speed, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, err
	}
	return speed, nil
}

func decodeIPv4Hex(value string) (string, error) {
	raw, err := strconv.ParseUint(strings.TrimSpace(value), 16, 32)
	if err != nil {
		return "", err
	}
	var bytes [4]byte
	binary.LittleEndian.PutUint32(bytes[:], uint32(raw))
	return net.IP(bytes[:]).String(), nil
}
