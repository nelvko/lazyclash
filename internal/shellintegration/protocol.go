package shellintegration

import (
	"fmt"
	"strconv"
	"strings"
)

func bit(value bool) int {
	if value {
		return 1
	}
	return 0
}

func parseBit(value string) (bool, error) {
	switch value {
	case "0":
		return false, nil
	case "1":
		return true, nil
	}
	return false, fmt.Errorf("invalid state flag")
}

func parseRequest(data []byte) (Request, error) {
	var result Request
	f := strings.Fields(string(data))
	if len(f) != 6 || f[0] != "1" {
		return result, fmt.Errorf("invalid terminal proxy request")
	}
	var err error
	result.Revision, err = strconv.ParseUint(f[1], 10, 64)
	if err != nil || result.Revision == 0 {
		return Request{}, fmt.Errorf("invalid request revision")
	}
	result.Enabled, err = parseBit(f[2])
	if err != nil {
		return Request{}, err
	}
	result.Host = f[3]
	if result.Host != "127.0.0.1" {
		return Request{}, fmt.Errorf("invalid proxy host")
	}
	result.Port, err = strconv.Atoi(f[4])
	if err != nil || result.Port < 0 || result.Port > 65535 || (result.Enabled && result.Port == 0) {
		return Request{}, fmt.Errorf("invalid proxy port")
	}
	result.Detach, err = parseBit(f[5])
	if err != nil {
		return Request{}, err
	}
	return result, nil
}

func requestBytes(r Request) []byte {
	return fmt.Appendf(nil, "1 %d %d %s %d %d\n", r.Revision, bit(r.Enabled), r.Host, r.Port, bit(r.Detach))
}

func parseACK(data []byte) (Acknowledgment, error) {
	var result Acknowledgment
	f := strings.Fields(string(data))
	if len(f) != 6 || f[0] != "1" {
		return result, fmt.Errorf("invalid terminal proxy acknowledgment")
	}
	var err error
	result.Revision, err = strconv.ParseUint(f[1], 10, 64)
	if err != nil || result.Revision == 0 {
		return result, fmt.Errorf("invalid acknowledgment revision")
	}
	result.Enabled, err = parseBit(f[2])
	if err != nil {
		return result, err
	}
	result.Port, err = strconv.Atoi(f[3])
	if err != nil || result.Port < 0 || result.Port > 65535 {
		return result, fmt.Errorf("invalid acknowledgment port")
	}
	result.Detached, err = parseBit(f[4])
	if err != nil {
		return result, err
	}
	result.Error, err = strconv.Atoi(f[5])
	if err != nil || result.Error < 0 || result.Error > 255 {
		return result, fmt.Errorf("invalid acknowledgment error")
	}
	return result, nil
}

func parseEndpoint(data []byte) (Endpoint, error) {
	f := strings.Fields(string(data))
	if len(f) != 4 || f[0] != "1" {
		return Endpoint{}, fmt.Errorf("invalid terminal proxy endpoint")
	}
	if _, err := strconv.ParseUint(f[1], 10, 64); err != nil {
		return Endpoint{}, fmt.Errorf("invalid endpoint revision")
	}
	port, err := strconv.Atoi(f[3])
	if err != nil {
		return Endpoint{}, err
	}
	endpoint := Endpoint{Host: f[2], Port: port}
	return endpoint, validEndpoint(endpoint, false)
}
