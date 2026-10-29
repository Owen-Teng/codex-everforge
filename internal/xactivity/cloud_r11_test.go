package xactivity

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type brokenStream struct{ sent bool }

func (b *brokenStream) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, `{"data":`), nil
	}
	return 0, errors.New("synthetic connection reset")
}
func (b *brokenStream) Close() error { return nil }
func TestTransportReadErrorIsRetryableNotSyntaxFailure(t *testing.T) {
	c := ReplayConfig()
	p, _ := NewProcessor(c, time.Now, "")
	r := Runner{Config: c, Processor: p}
	status := Status{}
	reason := r.read(context.Background(), &http.Response{Body: &brokenStream{}}, &status)
	if reason != "disconnected" || status.Notifications != 0 {
		t.Fatalf("transport failure: %s", reason)
	}
	reason = r.read(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(`{bad-json}`))}, &Status{})
	if reason != "invalid_stream_json" {
		t.Fatal("malformed JSON no longer fails closed")
	}
}
func TestOfficialOperationalDisconnectRetriesWithinBudget(t *testing.T) {
	r, closeServer := fixtureRunner(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"errors":[{"title":"operational-disconnect","disconnect_type":"UpstreamOperationalDisconnect","detail":"fixture-sensitive-do-not-log","type":"https://api.x.com/2/problems/operational-disconnect"}]}`)
	})
	defer closeServer()
	r.Config.MaxAttempts = 2
	status := r.Run(context.Background())
	if status.Attempts != 2 || status.Reason != "operational_disconnect" || status.ConnectionActive || status.Notifications != 0 {
		t.Fatalf("control frame handling: %+v", status)
	}
}
func TestSecretFileRestrictedAndRedacted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture-secret")
	if err := os.WriteFile(path, []byte("synthetic-not-a-real-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	secret, err := (SecretFileReader{Path: path}).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer secret.clear()
	if string(secret.value) != "synthetic-not-a-real-secret" {
		t.Fatal("secret bytes changed")
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, 0644)
		if _, err = (SecretFileReader{Path: path}).Read(context.Background()); err == nil {
			t.Fatal("world readable secret accepted")
		}
	}
	if _, err = (SecretFileReader{Path: "relative-path"}).Read(context.Background()); err == nil {
		t.Fatal("relative path accepted")
	}
}
