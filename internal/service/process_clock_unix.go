//go:build linux || darwin

package service

import (
	"time"

	"golang.org/x/sys/unix"
)

func processClock() (time.Time, time.Duration, error) {
	var clock unix.Timespec
	err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &clock)
	return time.Now(), time.Duration(clock.Nano()), err
}
