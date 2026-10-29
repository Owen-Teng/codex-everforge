package quota

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Settings struct {
	Interval   int    `json:"intervalSeconds"`
	Bucket     string `json:"selectedBucket,omitempty"`
	Executable string `json:"codexExecutable,omitempty"`
}
type Store struct {
	mu      sync.Mutex
	value   Settings
	path    string
	Warning string
}

func SettingsRoot() string {
	p, e := os.UserConfigDir()
	if e != nil {
		p = os.TempDir()
	}
	return filepath.Join(p, "CodexQuotaMonitor")
}
func OpenStore(root string) *Store {
	s := &Store{path: filepath.Join(root, "settings.json"), value: Settings{Interval: 120}}
	b, err := os.ReadFile(s.path)
	if err == nil {
		if json.Unmarshal(b, &s.value) != nil {
			s.value = Settings{Interval: 120}
			s.Warning = "设置文件无法读取，暂用默认值。"
		}
	}
	if s.value.Interval < 60 || s.value.Interval > 300 {
		s.value.Interval = 120
	}
	return s
}
func (s *Store) Get() Settings { s.mu.Lock(); defer s.mu.Unlock(); return s.value }
func (s *Store) Set(change func(*Settings)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(&s.value)
	if s.value.Interval < 60 {
		s.value.Interval = 60
	}
	if s.value.Interval > 300 {
		s.value.Interval = 300
	}
	b, _ := json.MarshalIndent(s.value, "", "  ")
	if os.MkdirAll(filepath.Dir(s.path), 0700) != nil {
		s.Warning = "设置无法保存，本次会话仍可使用。"
		return
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), "settings-*.tmp")
	if err != nil {
		s.Warning = "设置无法保存。"
		return
	}
	name := temp.Name()
	defer os.Remove(name)
	_ = temp.Chmod(0600)
	_, err = temp.Write(b)
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, s.path)
	}
	if err != nil {
		s.Warning = "设置无法保存，本次会话仍可使用。"
	} else {
		s.Warning = ""
	}
}
func (s *Store) GetWarning() string { s.mu.Lock(); defer s.mu.Unlock(); return s.Warning }
