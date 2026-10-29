package live

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// One preferences file is shared by bridge and the opted-in quota-core worker.
// No quota, identity, usage response or daily token value is written here.
func readPreferences(root string) (Preferences, error) {
	data, err := os.ReadFile(filepath.Join(root, "live-preferences.json"))
	if os.IsNotExist(err) {
		return Preferences{}, nil
	}
	if err != nil {
		return Preferences{}, errors.New("preferences_read")
	}
	var value struct {
		ResetAlertsEnabled *bool `json:"resetAlertsEnabled"`
	}
	if len(data) > 1024 || strictJSON(data, &value) != nil || value.ResetAlertsEnabled == nil {
		return Preferences{}, errors.New("preferences_invalid")
	}
	return Preferences{ResetAlertsEnabled: *value.ResetAlertsEnabled}, nil
}
func atomicJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("state_save")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("state_save")
	}
	file, err := os.CreateTemp(filepath.Dir(path), "live-*.tmp")
	if err != nil {
		return errors.New("state_save")
	}
	name := file.Name()
	defer os.Remove(name)
	_ = file.Chmod(0600)
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		return errors.New("state_save")
	}
	return nil
}
