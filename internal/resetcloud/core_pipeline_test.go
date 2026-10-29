package resetcloud

import (
	"bufio"
	"codex-quota-monitor/internal/live"
	"codex-quota-monitor/internal/quota"
	"codex-quota-monitor/internal/xactivity"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This runs the real core executable against the same cloud HTTP implementation
// used in production, fed through the actual X Runner and Processor. All input,
// local settings, quota and URLs here are synthetic and isolated.
func TestSyntheticXCloudActualCorePipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	name := "quota-core"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/quota-core")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("core build: %s %v", out, err)
	}
	service, err := Open(filepath.Join(t.TempDir(), "state.json"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	frames := make(chan []byte, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		flusher := w.(http.Flusher)
		beat := time.NewTicker(100 * time.Millisecond)
		defer beat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case frame := <-frames:
				mid := len(frame) / 2
				_, _ = w.Write(frame[:mid])
				flusher.Flush()
				_, _ = w.Write(append(frame[mid:], '\n'))
				flusher.Flush()
			case <-beat.C:
				_, _ = io.WriteString(w, "\r\n")
				flusher.Flush()
			}
		}
	}))
	defer upstream.Close()
	var reads atomic.Int32
	var holdFeed atomic.Bool
	resumeFeed := make(chan struct{})
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if holdFeed.Load() {
			select {
			case <-resumeFeed:
			case <-r.Context().Done():
				return
			}
		}
		service.ServeHTTP(w, r)
	}))
	defer cloud.Close()
	config := xactivity.ReplayConfig()
	processor, _ := xactivity.NewProcessor(config, time.Now, "")
	connector, _ := xactivity.NewLoopbackConnector(upstream.URL)
	runnerCtx, stopRunner := context.WithCancel(ctx)
	defer stopRunner()
	runner := xactivity.Runner{Config: config, Connector: connector, Processor: processor, OnStatus: func(s xactivity.Status) { _ = service.RecordStatus("", s) }, OnDecision: func(d xactivity.Decision) {
		if err := service.Ingest(d); err != nil {
			t.Error(err)
		}
	}}
	runnerDone := make(chan struct{})
	go func() { defer close(runnerDone); runner.Run(runnerCtx) }()
	core := exec.CommandContext(ctx, binary, "--demo", "ready", "--live", "--settings-root", t.TempDir(), "--reset-feed-test-url", cloud.URL+"/v1/feed")
	stdin, err := core.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := core.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = core.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if core.ProcessState == nil {
			_ = core.Process.Kill()
			_ = core.Wait()
		}
	}()
	type state struct {
		quota.Snapshot
		Reset        live.ResetState    `json:"reset"`
		Notification *live.Notification `json:"notification"`
		Preferences  live.Preferences   `json:"preferences"`
	}
	states := make(chan state, 128)
	go func() {
		defer close(states)
		scan := bufio.NewScanner(stdout)
		scan.Buffer(make([]byte, 4096), 1<<20)
		for scan.Scan() {
			var s state
			if json.Unmarshal(scan.Bytes(), &s) == nil {
				select {
				case states <- s:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	send := func(value any) {
		if err := json.NewEncoder(stdin).Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	await := func(label string, predicate func(state) bool) state {
		t.Helper()
		for {
			select {
			case <-ctx.Done():
				t.Fatalf("%s: pipeline deadline", label)
			case s, ok := <-states:
				if !ok {
					t.Fatalf("%s: core stopped", label)
				}
				if predicate(s) {
					return s
				}
			}
		}
	}
	send(map[string]any{"action": "reset_alerts", "enabled": true})
	baseline := await("silent baseline", func(s state) bool { return s.Reset.SourceFresh })
	if baseline.Notification != nil || baseline.Reset.SourceKind != "synthetic_cloud" {
		t.Fatal("initial baseline/mode incorrect")
	}
	now := time.Now()
	frame := xactivity.SyntheticFrame(config, "r11-pipeline-event", "987654321001", "We just reset Codex limits.", now, now)
	frames <- frame
	notice := await("new author claim", func(s state) bool { return s.Notification != nil && s.Notification.Kind == "announced" })
	if !strings.Contains(notice.Notification.Title, "联调") || notice.Reset.Event == nil || notice.Reset.Event.Verification != "author_claim" || notice.Notification.Kind == "recovered" {
		t.Fatal("synthetic author claim misrepresented")
	}
	id := notice.Notification.ID
	frames <- frame
	readsBefore := reads.Load()
	for reads.Load() < readsBefore+2 {
		select {
		case <-ctx.Done():
			t.Fatal("duplicate poll timeout")
		case s := <-states:
			if s.Notification != nil && s.Notification.ID != id {
				t.Fatal("duplicate created new notice")
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	// Withhold the next feed until the final captured event's upstream stops.
	holdFeed.Store(true)
	now = time.Now()
	frames <- xactivity.SyntheticFrame(config, "r11-last-event", "987654321002", "Codex limits will be reset tomorrow.", now, now)
	for len(service.Feed().Events) < 2 {
		select {
		case <-ctx.Done():
			t.Fatal("last event capture timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}
	stopRunner()
	<-runnerDone
	close(resumeFeed)
	last := await("captured final event after upstream stop", func(s state) bool {
		return s.Notification != nil && s.Notification.ID != id && s.Notification.Kind == "notice"
	})
	if !last.Reset.SourceFresh || last.Reset.Reason != "cloud_upstream_stopped_or_stale" || service.Feed().Upstream.Connected {
		t.Fatal("final event lost or upstream stopped hidden")
	}
	send(map[string]any{"action": "reset_alerts", "enabled": false})
	await("off acknowledged", func(s state) bool { return !s.Preferences.ResetAlertsEnabled })
	stoppedReads := reads.Load()
	timer := time.NewTimer(1500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal("off timeout")
	}
	if reads.Load() != stoppedReads {
		t.Fatal("disabled core still polls cloud")
	}
	send(map[string]any{"action": "refresh"})
	await("quota remains independent", func(s state) bool { return !s.Preferences.ResetAlertsEnabled && s.Demo && s.State != "loading" })
	send(map[string]any{"action": "quit"})
	if err := core.Wait(); err != nil {
		t.Fatal(err)
	}
	t.Log("synthetic HTTP X -> Runner -> Processor -> persistent Service -> actual core: silent baseline/new notice/dedupe/upstream stop/off/quota/quit passed; no desktop/cloud/live-X claim")
}
