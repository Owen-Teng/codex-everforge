//go:build !linux

package resetcloud

import (
	"errors"
)

func syncDirectory(string) error { return nil }

// The deployable receiver is Linux only; offline tests use Service directly.
func LockState(string) (func(), error) { return nil, errors.New("cloud_runtime_requires_linux") }
