package config

import (
	"strings"
	"testing"
	"time"
)

func TestDockerTaskPollInterval(t *testing.T) {
	t.Run("defaults to ten seconds", func(t *testing.T) {
		cfg, err := Load(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DockerTaskPollInterval != 10*time.Second {
			t.Errorf("interval = %v, want 10s", cfg.DockerTaskPollInterval)
		}
	})

	t.Run("zero turns it off", func(t *testing.T) {
		t.Setenv("CETACEAN_DOCKER_TASK_POLL_INTERVAL", "0s")

		cfg, err := Load(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DockerTaskPollInterval != 0 {
			t.Errorf("interval = %v, want 0", cfg.DockerTaskPollInterval)
		}
	})

	t.Run("the flag outranks the file", func(t *testing.T) {
		flag, file := "2s", "9s"
		cfg, err := Load(
			&fileConfig{Docker: &fileDocker{TaskPollInterval: &file}},
			&Flags{DockerTaskPoll: &flag},
		)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DockerTaskPollInterval != 2*time.Second {
			t.Errorf("interval = %v, want 2s", cfg.DockerTaskPollInterval)
		}
	})

	t.Run("a negative interval is refused", func(t *testing.T) {
		t.Setenv("CETACEAN_DOCKER_TASK_POLL_INTERVAL", "-1s")

		if _, err := Load(nil, nil); err == nil {
			t.Error("Load accepted a negative interval")
		}
	})

	t.Run("an interval under a second is refused", func(t *testing.T) {
		t.Setenv("CETACEAN_DOCKER_TASK_POLL_INTERVAL", "1ns")

		_, err := Load(nil, nil)
		if err == nil || !strings.Contains(err.Error(), "docker.task_poll_interval") {
			t.Errorf("Load(1ns) = %v, want an error naming the setting", err)
		}
	})

	t.Run("one second is accepted", func(t *testing.T) {
		t.Setenv("CETACEAN_DOCKER_TASK_POLL_INTERVAL", "1s")

		if _, err := Load(nil, nil); err != nil {
			t.Errorf("Load(1s): %v", err)
		}
	})
}
