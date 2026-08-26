package fasteval

import (
	"context"
	"fmt"
	"time"
)

func registerTimeBuiltins(functions map[string]*callable) {
	registerBuiltin(functions, "now", func(_ context.Context, arguments []any) (any, error) {
		err := checkArity(arguments, 0, 0)
		if err != nil {
			return nil, err
		}

		return time.Now(), nil
	})
	registerBuiltin(functions, "duration", durationBuiltin)
	registerBuiltin(functions, "date", dateBuiltin)
	registerBuiltin(functions, "timezone", timezoneBuiltin)
}

func durationBuiltin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	text, valid := arguments[0].(string)
	if !valid {
		return nil, newDetailError("duration argument has type %T, want string", arguments[0])
	}

	duration, err := time.ParseDuration(text)
	if err != nil {
		return nil, fmt.Errorf("parse duration: %w", err)
	}

	return duration, nil
}

func dateBuiltin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, threeArguments)
	if err != nil {
		return nil, err
	}

	text, valid := arguments[0].(string)
	if !valid {
		return nil, newDetailError("date argument has type %T, want string", arguments[0])
	}

	if len(arguments) == 1 {
		return parseKnownDate(text)
	}

	if len(arguments) == twoArguments {
		return parseDateLayout(text, arguments[1])
	}

	return parseDateLocation(text, arguments[1], arguments[2])
}

func parseKnownDate(text string) (any, error) {
	layouts := []string{time.RFC3339Nano, time.RFC3339, time.DateOnly}
	for _, layout := range layouts {
		value, err := time.Parse(layout, text)
		if err == nil {
			return value, nil
		}
	}

	return nil, newDetailError("date %q is not RFC3339 or YYYY-MM-DD", text)
}

func parseDateLayout(text string, rawLayout any) (any, error) {
	layout, valid := rawLayout.(string)
	if !valid {
		return nil, newDetailError("date layout has type %T, want string", rawLayout)
	}

	value, err := time.Parse(layout, text)
	if err != nil {
		return nil, fmt.Errorf("parse date: %w", err)
	}

	return value, nil
}

func parseDateLocation(text string, rawLayout, rawZone any) (any, error) {
	layout, valid := rawLayout.(string)
	if !valid {
		return nil, newDetailError("date layout has type %T, want string", rawLayout)
	}

	zone, valid := rawZone.(string)
	if !valid {
		return nil, newDetailError("date timezone has type %T, want string", rawZone)
	}

	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("load date timezone: %w", err)
	}

	value, err := time.ParseInLocation(layout, text, location)
	if err != nil {
		return nil, fmt.Errorf("parse date in timezone: %w", err)
	}

	return value, nil
}

func timezoneBuiltin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	name, ok := arguments[0].(string)
	if !ok {
		return nil, newDetailError("timezone argument has type %T, want string", arguments[0])
	}

	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("load timezone: %w", err)
	}

	return location, nil
}
