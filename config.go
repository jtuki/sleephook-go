package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type configFile struct {
	Message     string       `yaml:"message"`
	Speed       int          `yaml:"speed"`
	Opacity     int          `yaml:"opacity"`
	LockPeriods []lockPeriod `yaml:"lock_periods"`
}

type lockPeriod struct {
	Start string `yaml:"start" json:"start"`
	End   string `yaml:"end" json:"end"`
}

// TimeRange represents a lock period as seconds since midnight.
type TimeRange struct {
	StartSec int
	StopSec  int
}

func loadConfig(path string) ([]TimeRange, string, int, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", 0, 0, fmt.Errorf("read config: %w", err)
	}

	var cfg configFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, "", 0, 0, fmt.Errorf("parse config: %w", err)
	}

	if len(cfg.LockPeriods) == 0 {
		return nil, "", 0, 0, fmt.Errorf("no lock_periods configured")
	}

	msg := cfg.Message
	if msg == "" {
		msg = "不熬夜！早点休息！"
	}

	var ranges []TimeRange
	for i, p := range cfg.LockPeriods {
		start, err := parseTimeOfDay(p.Start)
		if err != nil {
			return nil, "", 0, 0, fmt.Errorf("lock_periods[%d].start: %w", i, err)
		}
		stop, err := parseTimeOfDay(p.End)
		if err != nil {
			return nil, "", 0, 0, fmt.Errorf("lock_periods[%d].end: %w", i, err)
		}
		// Overnight ranges must not exceed 1 hour
		if start > stop {
			duration := (86400 - start + stop)
			if duration > 3600 {
				return nil, "", 0, 0, fmt.Errorf(
					"lock_periods[%d] %s-%s: overnight range is %d min, max is 60 min",
					i, p.Start, p.End, duration/60)
			}
		}
		ranges = append(ranges, TimeRange{start, stop})
	}
	speed := cfg.Speed
	if speed < 1 {
		speed = 2
	}
	opacity := cfg.Opacity
	if opacity < 1 || opacity > 255 {
		opacity = 240
	}
	return ranges, msg, speed, opacity, nil
}

func parseTimeOfDay(s string) (int, error) {
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Hour()*3600 + t.Minute()*60 + t.Second(), nil
		}
	}
	return 0, fmt.Errorf("invalid time %q (use hh:mm or hh:mm:ss)", s)
}

func shouldLock(now time.Time, ranges []TimeRange) bool {
	sec := now.Hour()*3600 + now.Minute()*60 + now.Second()
	for _, tr := range ranges {
		if tr.StartSec <= tr.StopSec {
			if sec >= tr.StartSec && sec < tr.StopSec {
				return true
			}
		} else {
			// overnight range (e.g. 23:00-07:00)
			if sec >= tr.StartSec || sec < tr.StopSec {
				return true
			}
		}
	}
	return false
}

func configPath() string {
	exe, err := os.Executable()
	if err == nil {
		p := filepath.Join(filepath.Dir(exe), "config.yaml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "config.yaml"
}
