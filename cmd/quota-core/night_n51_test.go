package main

import (
	"bufio"
	"codex-quota-monitor/internal/live"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A real worker process reads the retained real public fixture beside entirely
// synthetic quota data. It performs no source/Codex requests and opens no GUI.
func TestN51ActualCoreRetainedRealHistorySilentAndIndependent(t *testing.T) {
	t.Skip("Private external capture excluded from public source; synthetic regressions remain enabled.")
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	root := t.TempDir()
	fixtureRoot := filepath.Join("..", "..", "community", "backend", "night-n51")
	raw, err := os.ReadFile(filepath.Join(fixtureRoot, "aihot-recent-real.json"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := os.ReadFile(filepath.Join(fixtureRoot, "aihot-recent-real-meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = live.SeedAIHOT(filepath.Join(root, "demo"), raw, meta); err != nil {
		t.Fatal(err)
	}
	name := "quota-core"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", binary, "./cmd/quota-core")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %s %v", out, err)
	}
	command := exec.CommandContext(ctx, binary, "--demo=75", "--live", "--settings-root", root)
	input, _ := command.StdinPipe()
	output, _ := command.StdoutPipe()
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		input.Close()
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	lines := make(chan []byte, 20)
	go func() {
		defer close(lines)
		scan := bufio.NewScanner(output)
		scan.Buffer(make([]byte, 4096), 262144)
		for scan.Scan() {
			copyLine := append([]byte(nil), scan.Bytes()...)
			select {
			case lines <- copyLine:
			case <-ctx.Done():
				return
			}
		}
	}()
	verified := false
	for !verified {
		select {
		case <-ctx.Done():
			t.Fatal("core history deadline")
		case line, ok := <-lines:
			if !ok {
				t.Fatal("core exited before history")
			}
			var d struct {
				State        string `json:"state"`
				Demo         bool   `json:"demo"`
				Notification any    `json:"notification"`
				Inbox        struct {
					Source struct {
						Origin string `json:"origin"`
					} `json:"source"`
					Messages []struct {
						PostID      string `json:"postId"`
						Category    string `json:"category"`
						PublishedAt *int64 `json:"publishedAt"`
					} `json:"messages"`
				} `json:"inbox"`
			}
			if json.Unmarshal(line, &d) != nil {
				continue
			}
			if d.State != "ready" {
				continue
			}
			if !d.Demo || d.Notification != nil || d.Inbox.Source.Origin != "manual_public_import" {
				t.Fatal("history mislabeled or alerted")
			}
			for _, m := range d.Inbox.Messages {
				if m.PostID == "2114588174249071102" && m.Category == "signal" && m.PublishedAt != nil {
					verified = true
				}
			}
		}
	}
	if err = json.NewEncoder(input).Encode(map[string]string{"action": "quit"}); err != nil {
		t.Fatal(err)
	}
	if err = command.Wait(); err != nil {
		t.Fatal(err)
	}
	t.Log("actual core retained real Day21 as scope-unknown signal; silent historical import; synthetic quota unchanged; no network/GUI; quit cleanly")
}
