package quota

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"time"
)

type Reader interface {
	Read(context.Context, Settings) (Snapshot, error)
}
type Provider struct{}
type rpcMessage struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		RetryAfterSeconds int `json:"retryAfterSeconds"`
	} `json:"data"`
}
type rpcClient struct {
	writer   io.Writer
	messages chan rpcMessage
	id       int
	ctx      context.Context
}

func (c *rpcClient) call(method string, params any, out any) error {
	c.id++
	id := c.id
	request := map[string]any{"id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	if json.NewEncoder(c.writer).Encode(request) != nil {
		return Fail("network")
	}
	for {
		select {
		case <-c.ctx.Done():
			if c.ctx.Err() == context.DeadlineExceeded {
				return Fail("timeout")
			}
			return Fail("cancelled")
		case m, ok := <-c.messages:
			if !ok {
				return Fail("network")
			}
			if m.Method != "" && m.ID != 0 {
				_ = json.NewEncoder(c.writer).Encode(map[string]any{"id": m.ID, "error": map[string]any{"code": -32601, "message": "Read-only quota client"}})
				continue
			}
			if m.ID != id {
				continue
			}
			if m.Error != nil {
				return classifyRPC(m.Error)
			}
			if out != nil && json.Unmarshal(m.Result, out) != nil {
				return Fail("invalid_data")
			}
			return nil
		}
	}
}
func classifyRPC(e *rpcError) error {
	s := strings.ToLower(e.Message)
	kind := "unknown"
	switch {
	case e.Code == -32601:
		kind = "unsupported_version"
	case strings.Contains(s, "429") || strings.Contains(s, "too many") || strings.Contains(s, "rate limit"):
		kind = "rate_limited"
	case strings.Contains(s, "401") || strings.Contains(s, "unauthorized") || strings.Contains(s, "authentication") || strings.Contains(s, "login") || strings.Contains(s, "log in"):
		kind = "auth"
	case strings.Contains(s, "chatgpt") && strings.Contains(s, "required"):
		kind = "unsupported_account"
	case strings.Contains(s, "connect") || strings.Contains(s, "network") || strings.Contains(s, "fetch") || strings.Contains(s, "request"):
		kind = "network"
	}
	retry := time.Duration(0)
	if e.Data.RetryAfterSeconds > 0 && e.Data.RetryAfterSeconds <= 86400 {
		retry = time.Duration(e.Data.RetryAfterSeconds) * time.Second
	}
	return &Failure{Kind: kind, RetryAfter: retry}
}

type accountResponse struct {
	Account json.RawMessage `json:"account"`
}

func accountKey(a json.RawMessage) (string, error) {
	var fields struct {
		Type  string  `json:"type"`
		Email *string `json:"email"`
	}
	if len(a) == 0 || string(a) == "null" {
		return "", Fail("not_logged_in")
	}
	if json.Unmarshal(a, &fields) != nil {
		return "", Fail("invalid_data")
	}
	if fields.Type != "chatgpt" {
		return "", Fail("unsupported_account")
	}
	if fields.Email == nil || *fields.Email == "" {
		return "", nil
	}
	sum := sha256.Sum256([]byte(fields.Type + ":" + *fields.Email))
	return hex.EncodeToString(sum[:]), nil
}
func (Provider) Read(ctx context.Context, settings Settings) (Snapshot, error) {
	exe, err := FindCodex(settings.Executable)
	if err != nil {
		return Snapshot{}, err
	}
	return readProcess(ctx, exe, []string{"app-server"}, settings.Bucket)
}
func withRPC(parent context.Context, exe string, args []string, use func(*rpcClient) error) error {
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	configureProcess(cmd)
	stdin, e := cmd.StdinPipe()
	if e != nil {
		return Fail("unknown")
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return Fail("unknown")
	}
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		return Fail("not_installed")
	}
	release, e := guardProcess(cmd)
	if e != nil {
		cancel()
		_ = cmd.Wait()
		return Fail("unknown")
	}
	defer release()
	c := &rpcClient{writer: stdin, messages: make(chan rpcMessage, 8), ctx: ctx}
	go func() {
		defer close(c.messages)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 65536), 2*1024*1024)
		for scanner.Scan() {
			var m rpcMessage
			if json.Unmarshal(scanner.Bytes(), &m) != nil {
				continue
			}
			select {
			case c.messages <- m:
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() { _ = stdin.Close(); cancel(); _ = cmd.Wait() }()
	if err := c.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "codex_quota_monitor", "title": "Codex Quota Monitor", "version": "0.4.0"}}, nil); err != nil {
		return err
	}
	if json.NewEncoder(stdin).Encode(map[string]string{"method": "initialized"}) != nil {
		return Fail("network")
	}
	return use(c)
}

func readProcess(parent context.Context, exe string, args []string, bucket string) (snapshot Snapshot, err error) {
	err = withRPC(parent, exe, args, func(c *rpcClient) error {
		var before accountResponse
		if err = c.call("account/read", map[string]bool{"refreshToken": false}, &before); err != nil {
			return err
		}
		key, e := accountKey(before.Account)
		if e != nil {
			return e
		}
		snapshot.AccountKey = key
		snapshot.Bucket = bucket
		if snapshot.Bucket == "" {
			snapshot.Bucket = "codex"
		}
		var raw Response
		if err = c.call("account/rateLimits/read", nil, &raw); err != nil {
			return err
		}
		var after accountResponse
		if err = c.call("account/read", map[string]bool{"refreshToken": false}, &after); err != nil {
			return err
		}
		afterKey, e := accountKey(after.Account)
		if e != nil {
			return e
		}
		if key != afterKey || string(before.Account) != string(after.Account) {
			snapshot = Snapshot{}
			return Fail("account_changed")
		}
		snapshot, err = Normalize(raw, bucket)
		snapshot.AccountKey = key
		if key == "" && raw.AccountID != nil && *raw.AccountID != "" {
			sum := sha256.Sum256([]byte(*raw.AccountID))
			snapshot.AccountKey = hex.EncodeToString(sum[:])
		}
		// Never persist or serialize account identity, raw account, credentials, or response.
		return err
	})
	return snapshot, err
}
