package notify

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
)

func LoadRules(path string) ([]Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var rules []Rule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, err
	}

	for i := range rules {
		if err := rules[i].compile(); err != nil {
			return nil, err
		}
	}

	return rules, nil
}
