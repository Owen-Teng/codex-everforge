package quota

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

// Exercise the real scheduler without sending any refresh command.
func TestAutomaticRefreshWithoutManualInput(t *testing.T) {
	store := OpenStore(t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		engine := NewEngine(&DemoReader{Scenario: "75"}, store, true)
		var ready []Snapshot
		go engine.Run(ctx, func(s Snapshot) {
			if s.State == "ready" {
				ready = append(ready, s)
			}
		})
		time.Sleep(time.Second)
		synctest.Wait()
		if len(ready) != 1 {
			t.Fatalf("startup reads: %d", len(ready))
		}
		first := ready[0]
		if first.NextRefresh-first.LastSuccess != 120 {
			t.Fatal("wrong scheduled interval")
		}
		time.Sleep(119 * time.Second)
		synctest.Wait()
		if len(ready) != 1 {
			t.Fatal("refreshed before timer deadline")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if len(ready) != 2 || ready[1].LastSuccess <= first.LastSuccess {
			t.Fatal("no automatic second refresh")
		}
		cancel()
		synctest.Wait()
	})
}
