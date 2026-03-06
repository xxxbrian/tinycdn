package model

import (
	"fmt"
	"time"
)

func validatePositiveDuration(raw string) error {
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return err
	}
	if duration <= 0 {
		return fmt.Errorf("duration must be greater than zero")
	}

	return nil
}

func ParseOptionalDuration(raw string) (time.Duration, bool, error) {
	if raw == "" {
		return 0, false, nil
	}

	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false, err
	}
	if duration <= 0 {
		return 0, false, fmt.Errorf("duration must be greater than zero")
	}

	return duration, true, nil
}
