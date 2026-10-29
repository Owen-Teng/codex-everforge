package main

import (
	"bufio"
	"codex-quota-monitor/internal/live"
	"codex-quota-monitor/internal/messageinbox"
	"codex-quota-monitor/internal/quota"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
)

// Set only by the release builder after deployment/HTTPS acceptance.
var PublishedResetFeedURL string

func main() {
	demo := flag.String("demo", "", "Use synthetic quota data; never connects to Codex")
	once := flag.Bool("once", false, "Read once and print normalized quota (contains personal usage; do not publish)")
	check := flag.Bool("check", false, "Read once and print only availability, not usage values")
	liveMode := flag.Bool("live", false, "Enable local R2 bridge and reset alerts in this worker")
	staticRoot := flag.String("static-root", "", "R2 static assets; default is preview/community-mvp beside the executable")
	root := flag.String("settings-root", quota.SettingsRoot(), "Local settings directory")
	resetTestURL := flag.String("reset-feed-test-url", "", "Synthetic loopback feed; requires --demo")
	flag.Parse()
	if *resetTestURL != "" && (*demo == "" || !*liveMode) {
		fmt.Fprintln(os.Stderr, "reset_feed_test_requires_demo_live")
		os.Exit(2)
	}
	if *demo != "" {
		*root = filepath.Join(*root, "demo")
	}
	store := quota.OpenStore(*root)
	var reader quota.Reader = quota.Provider{}
	var fake *quota.DemoReader
	if *demo != "" {
		fake = &quota.DemoReader{Scenario: *demo}
		reader = fake
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if *once || *check {
		s, err := reader.Read(ctx, store.Get())
		if *check {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"available": err == nil, "windowCount": len(s.Windows), "state": s.State, "demo": *demo != ""})
			if err != nil {
				fmt.Fprintln(os.Stderr, quota.Reason(err))
				os.Exit(1)
			}
			return
		}
		if err != nil {
			s.State = "error"
			s.Color = "gray"
			s.Message = quota.Reason(err)
		}
		s.Demo = *demo != ""
		_ = json.NewEncoder(os.Stdout).Encode(s)
		if err != nil {
			os.Exit(1)
		}
		return
	}
	engine := quota.NewEngine(reader, store, *demo != "")
	var manager *live.Manager
	if *liveMode {
		var source live.Source
		var sourceErr error
		var internalErr error
		if *resetTestURL != "" {
			source, sourceErr = live.NewSyntheticCloudSource(*resetTestURL)
		} else if *demo == "" {
			internalSource, err := live.LoadPublicResetSources(*root)
			internalErr = err
			if internalSource != nil {
				source = internalSource
			} else {
				source, sourceErr = live.NewCloudSource(PublishedResetFeedURL)
			}
		}
		if sourceErr != nil {
			fmt.Fprintln(os.Stderr, "reset_feed_endpoint_invalid")
			os.Exit(2)
		}
		manager = live.NewManager(*root, source)
		if internalErr != nil {
			manager.SetRuntimeWarning("公开消息源配置不可用：" + internalErr.Error())
		}
		manager.SetRefresh(engine.Refresh)
		if *staticRoot == "" {
			executable, _ := os.Executable()
			*staticRoot = filepath.Join(filepath.Dir(executable), "preview", "community-mvp")
		}
		if err := live.StartBridge(ctx, manager, *staticRoot, "127.0.0.1:43188", engine.Refresh); err != nil {
			fmt.Fprintln(os.Stderr, "R2 bridge unavailable: 127.0.0.1:43188; quota worker continues")
			manager.SetRuntimeWarning("本机网页端口 43188 暂不可用，额度读取继续。")
		}
	}
	var writeMu sync.Mutex
	emit := func(s quota.Snapshot) {
		writeMu.Lock()
		defer writeMu.Unlock()
		var output any = s
		if manager != nil {
			for _, state := range manager.WorkerStates() {
				if !manager.WorkerStateCurrent(state) {
					continue
				}
				output = struct {
					quota.Snapshot
					Reset        live.ResetState        `json:"reset"`
					Usage        quota.UsageStatus      `json:"usage"`
					Preferences  live.Preferences       `json:"preferences"`
					Notification *live.Notification     `json:"notification"`
					Inbox        *messageinbox.Snapshot `json:"inbox,omitempty"`
				}{state.Quota, state.Reset, state.Usage, state.Preferences, state.Notification, state.Inbox}
				if json.NewEncoder(os.Stdout).Encode(output) != nil {
					cancel()
					return
				}
				if state.Notification != nil {
					manager.NotificationEmitted(state.Notification.ID)
				}
			}
			return
		}
		if json.NewEncoder(os.Stdout).Encode(output) != nil {
			cancel()
		}
	}
	if manager != nil {
		// Native notification hosts deliberately suppress their initial frame.
		// Write the empty startup baseline before any async source/usage work can
		// publish a newly eligible retained event and consume its durable receipt.
		emit(engine.Current())
		go manager.Run(ctx, store, *demo == "")
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-manager.Changed():
					emit(engine.Current())
				}
			}
		}()
	}
	go func() {
		scan := bufio.NewScanner(os.Stdin)
		scan.Buffer(make([]byte, 4096), 65536)
		for scan.Scan() {
			var c struct {
				Action     string `json:"action"`
				Seconds    int    `json:"seconds"`
				Bucket     string `json:"bucket"`
				Executable string `json:"executable"`
				Scenario   string `json:"scenario"`
				Enabled    *bool  `json:"enabled"`
				ID         string `json:"id"`
				Status     string `json:"status"`
				Reason     string `json:"reason"`
			}
			if json.Unmarshal(scan.Bytes(), &c) != nil {
				continue
			}
			switch c.Action {
			case "notification_result":
				if manager != nil && manager.RecordNotificationResult(c.ID, c.Status, c.Reason) != nil {
					fmt.Fprintln(os.Stderr, "notification_result_not_saved")
				}
			case "reset_alerts":
				if manager != nil && c.Enabled != nil {
					if manager.SetAlerts(*c.Enabled) != nil {
						fmt.Fprintln(os.Stderr, "live_preferences_not_saved")
					}
				}
			case "test_notification":
				if manager != nil && manager.TestNotification() != nil {
					fmt.Fprintln(os.Stderr, "live_test_notification_unavailable")
				}
			case "quit":
				cancel()
				return
			case "refresh":
				engine.Refresh()
			case "interval":
				if c.Seconds >= 60 && c.Seconds <= 300 {
					store.Set(func(s *quota.Settings) { s.Interval = c.Seconds })
					engine.Refresh()
				}
			case "select":
				if len(c.Bucket) <= 256 {
					store.Set(func(s *quota.Settings) { s.Bucket = c.Bucket })
					engine.Refresh()
				}
			case "executable":
				if len(c.Executable) <= 32767 {
					store.Set(func(s *quota.Settings) { s.Executable = c.Executable })
					engine.Refresh()
				}
			case "demo":
				if fake != nil {
					fake.Set(c.Scenario)
					engine.Refresh()
				}
			}
		}
		cancel()
	}()
	engine.Run(ctx, func(s quota.Snapshot) {
		if manager != nil {
			manager.ObserveQuota(s)
		} else {
			emit(s)
		}
	})
}
