//go:build !linux && !darwin

package service

import (
	"errors"
	"time"
)

func processClock() (time.Time, time.Duration, error) {
	return time.Time{}, 0, errors.New("systemd process clock unavailable")
}
