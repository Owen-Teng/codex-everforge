//go:build windows

package xactivity

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"unicode/utf16"
	"unsafe"
)

type credentialW struct {
	Flags          uint32
	Type           uint32
	Target         *uint16
	Comment        *uint16
	Written        windows.Filetime
	BlobSize       uint32
	Blob           *byte
	Persist        uint32
	AttributeCount uint32
	Attributes     uintptr
	TargetAlias    *uint16
	UserName       *uint16
}

func (CredentialManager) Read(ctx context.Context) (Secret, error) {
	if ctx.Err() != nil {
		return Secret{}, errors.New("cancelled")
	}
	// Fixed generic credential, current logon session, read only. This method is
	// not called by replay/tests or while live/billing gates are off.
	dll := windows.NewLazySystemDLL("advapi32.dll")
	read := dll.NewProc("CredReadW")
	free := dll.NewProc("CredFree")
	target, _ := windows.UTF16PtrFromString(CredentialTarget)
	var credential *credentialW
	result, _, _ := read.Call(uintptr(unsafe.Pointer(target)), 1, 0, uintptr(unsafe.Pointer(&credential)))
	if result == 0 || credential == nil {
		return Secret{}, errors.New("not_configured")
	}
	defer free.Call(uintptr(unsafe.Pointer(credential)))
	if credential.Blob == nil || credential.BlobSize == 0 || credential.BlobSize > 2560 {
		return Secret{}, errors.New("credential_invalid")
	}
	blob := unsafe.Slice(credential.Blob, int(credential.BlobSize))
	defer func() {
		for i := range blob {
			blob[i] = 0
		}
	}()
	data := append([]byte(nil), blob...)
	defer func() {
		for i := range data {
			data[i] = 0
		}
	}()
	// Generic credential blobs are app-defined. Support ASCII/UTF-8 token bytes
	// and the UTF-16LE password representation used by the Windows UI.
	if len(data)%2 == 0 && len(data) > 1 && data[1] == 0 {
		units := make([]uint16, len(data)/2)
		for i := range units {
			units[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
		}
		return newSecret([]byte(string(utf16.Decode(units))))
	}
	return newSecret(data)
}
