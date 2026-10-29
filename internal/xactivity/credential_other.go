//go:build !windows

package xactivity

import (
	"context"
	"errors"
)

func (CredentialManager) Read(context.Context) (Secret, error) {
	return Secret{}, errors.New("not_configured_on_this_platform")
}
