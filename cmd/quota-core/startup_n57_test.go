package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Exercise the real stdout protocol rather than a second copy of the emitter.
// Source/usage are disabled by --demo; all files live under a fresh temp root.
func TestN57ActualCoreFirstFrameIsSilentLoadingBaseline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	name := "quota-core"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", binary, "./cmd/quota-core")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	command := exec.CommandContext(ctx, binary, "--demo=75", "--live", "--settings-root", t.TempDir())
	input, _ := command.StdinPipe()
	output, _ := command.StdoutPipe()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		input.Close()
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	scan := bufio.NewScanner(output)
	scan.Buffer(make([]byte, 4096), 262144)
	if !scan.Scan() {
		t.Fatal("missing first stdout", scan.Err())
	}
	var first struct {
		State        string          `json:"state"`
		Notification json.RawMessage `json:"notification"`
		Reset        struct {
			State string `json:"state"`
		} `json:"reset"`
	}
	if json.Unmarshal(scan.Bytes(), &first) != nil || first.State != "loading" || string(first.Notification) != "null" || first.Reset.State != "source_unavailable" {
		t.Fatalf("unsafe first frame %s", scan.Bytes())
	}
	ready := false
	for scan.Scan() {
		var next struct {
			State        string `json:"state"`
			Demo         bool   `json:"demo"`
			Notification any    `json:"notification"`
		}
		if json.Unmarshal(scan.Bytes(), &next) == nil && next.State == "ready" {
			if !next.Demo || next.Notification != nil {
				t.Fatal("baseline polluted later quota")
			}
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("no quota frame after baseline")
	}
	if err := json.NewEncoder(input).Encode(map[string]string{"action": "quit"}); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
}
