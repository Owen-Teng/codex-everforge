package live

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestN51InboxHTTPAndSubmissionReceiptStayLocal(t *testing.T) {
	now := time.Now()
	m := NewManager(t.TempDir(), &publicIdentitySource{})
	_ = m.SetAlerts(true)
	m.enabledAt = now.Add(-time.Hour).Unix()
	m.ApplySource(publicBatch(now), nil, m.generation, now)
	m.ApplySource(publicBatch(now, publicPost("74001", "Day 22/ Today is a reset.", now)), nil, m.generation, now)
	note := m.Snapshot().Notification
	if note == nil {
		t.Fatal("missing fixture notice")
	}
	bridge := &Bridge{Manager: m}
	server := httptest.NewServer(bridge)
	defer server.Close()
	bridge.Host = strings.TrimPrefix(server.URL, "http://")
	res, err := http.Get(server.URL + "/api/live/inbox")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(raw, []byte(`"postId":"74001"`)) || !bytes.Contains(raw, []byte(`"category":"signal"`)) {
		t.Fatal("inbox read failed")
	}
	bad, _ := http.NewRequest("GET", server.URL+"/api/live/inbox", nil)
	bad.Header.Set("Origin", "https://untrusted.invalid")
	res, err = http.DefaultClient.Do(bad)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("cross-origin inbox read allowed")
	}
	body, _ := json.Marshal(map[string]string{"id": note.ID, "status": "submitted", "reason": "synthetic_test_no_windows_submission"})
	req, _ := http.NewRequest("POST", server.URL+"/api/live/notification-result", bytes.NewReader(body))
	req.Header.Set("Origin", server.URL)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("local receipt rejected")
	}
	record, _ := m.inbox.Message("74001")
	if record.Delivery.State != "submitted" || record.Delivery.ResultAt == nil || record.Delivery.Reason != "synthetic_test_no_windows_submission" {
		t.Fatal("receipt did not persist exact evidence level")
	}
}
