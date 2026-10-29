package xactivity

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// SecretFileReader reads an operator-mounted secret, never an environment value
// or Windows credential store. Callers must pass authorization gates first.
type SecretFileReader struct{ Path string }

func (r SecretFileReader) Read(ctx context.Context) (Secret, error) {
	fail := func() (Secret, error) { return Secret{}, errors.New("secret_file_unavailable") }
	if ctx.Err() != nil || !filepath.IsAbs(r.Path) {
		return fail()
	}
	info, err := os.Lstat(r.Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2562 || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return fail()
	}
	f, err := os.Open(r.Path)
	if err != nil {
		return fail()
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || runtime.GOOS!="windows"&&opened.Mode().Perm()&0077!=0 {
		return fail()
	}
	raw, err := io.ReadAll(io.LimitReader(f, 2563))
	if err != nil || len(raw) > 2562 || ctx.Err() != nil {
		return fail()
	}
	defer func() {
		for i := range raw {
			raw[i] = 0
		}
	}()
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	if len(raw) > 0 && raw[len(raw)-1] == '\r' {
		raw = raw[:len(raw)-1]
	}
	secret, err := newSecret(raw)
	if err != nil {
		return fail()
	}
	return secret, nil
}
