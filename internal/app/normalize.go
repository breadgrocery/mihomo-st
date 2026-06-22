package app

import (
	"fmt"

	"mihomo-st/internal/config"
	"mihomo-st/internal/httpclient"
	"mihomo-st/internal/tester"
)

func NormalizeDelayCommand(cmd DelayCommand, cfg config.Config) (tester.DelayPlan, error) {
	targets := cmd.URLs
	if len(targets) == 0 {
		targets = delayConfigTargets(cfg.Delay.URLs)
	}

	plan := tester.DelayPlan{Targets: make([]tester.DelayTarget, 0, len(targets))}
	for _, target := range targets {
		normalized, err := normalizeDelayTargetCommand(target, cmd, cfg)
		if err != nil {
			return tester.DelayPlan{}, err
		}
		plan.Targets = append(plan.Targets, normalized)
	}
	return plan, nil
}

func NormalizeDownloadCommand(cmd DownloadCommand, cfg config.Config) (tester.DownloadPlan, error) {
	targets := cmd.URLs
	if len(targets) == 0 {
		targets = downloadConfigTargets(cfg.Download.URLs)
	}

	plan := tester.DownloadPlan{Targets: make([]tester.DownloadTarget, 0, len(targets))}
	for _, target := range targets {
		normalized, err := normalizeDownloadTargetCommand(target, cmd, cfg)
		if err != nil {
			return tester.DownloadPlan{}, err
		}
		plan.Targets = append(plan.Targets, normalized)
	}
	return plan, nil
}

func NormalizeDelayCollectionCommand(cmd DelayCollectionCommand, cfg config.Config) (tester.DelayPlan, int, error) {
	plan, err := NormalizeDelayCommand(cmd.DelayCommand, cfg)
	if err != nil {
		return tester.DelayPlan{}, 0, err
	}
	concurrency, err := firstInt("concurrency", cmd.Concurrency, intValue(cfg.Delay.Concurrency))
	if err != nil {
		return tester.DelayPlan{}, 0, err
	}
	return plan, concurrency, nil
}

func NormalizeDownloadCollectionCommand(cmd DownloadCollectionCommand, cfg config.Config) (tester.DownloadPlan, int, error) {
	plan, err := NormalizeDownloadCommand(cmd.DownloadCommand, cfg)
	if err != nil {
		return tester.DownloadPlan{}, 0, err
	}
	concurrency, err := firstInt("concurrency", cmd.Concurrency, intValue(cfg.Download.Concurrency))
	if err != nil {
		return tester.DownloadPlan{}, 0, err
	}
	return plan, concurrency, nil
}

func normalizeDelayTargetCommand(target DelayTargetCommand, cmd DelayCommand, cfg config.Config) (tester.DelayTarget, error) {
	if err := validateProxyRequestURL(target.URL); err != nil {
		return tester.DelayTarget{}, err
	}
	timeout, err := firstInt("timeout", target.Timeout, cmd.Timeout, intValue(cfg.Delay.Timeout))
	if err != nil {
		return tester.DelayTarget{}, err
	}
	expected, err := firstString("expected", target.Expected, cmd.Expected, stringValue(cfg.Delay.Expected))
	if err != nil {
		return tester.DelayTarget{}, err
	}
	rounds, err := firstInt("rounds", target.Rounds, cmd.Rounds, intValue(cfg.Delay.Rounds))
	if err != nil {
		return tester.DelayTarget{}, err
	}

	return tester.DelayTarget{
		URL:            target.URL,
		Timeout:        timeout,
		Headers:        httpclient.MergeHeaders(cfg.Delay.Headers, cmd.Headers, target.Headers),
		FollowRedirect: firstBool(target.FollowRedirect, cmd.FollowRedirect, boolValuePtr(cfg.Delay.FollowRedirect)),
		Expected:       expected,
		Rounds:         rounds,
		Unified:        firstBool(target.Unified, cmd.Unified, boolValuePtr(cfg.Delay.Unified)),
	}, nil
}

func normalizeDownloadTargetCommand(target DownloadTargetCommand, cmd DownloadCommand, cfg config.Config) (tester.DownloadTarget, error) {
	if err := validateProxyRequestURL(target.URL); err != nil {
		return tester.DownloadTarget{}, err
	}
	timeout, err := firstInt("timeout", target.Timeout, cmd.Timeout, intValue(cfg.Download.Timeout))
	if err != nil {
		return tester.DownloadTarget{}, err
	}
	rounds, err := firstInt("rounds", target.Rounds, cmd.Rounds, intValue(cfg.Download.Rounds))
	if err != nil {
		return tester.DownloadTarget{}, err
	}
	maxBytes, err := firstInt("max-bytes", target.MaxBytes, cmd.MaxBytes, intValue(cfg.Download.MaxBytes))
	if err != nil {
		return tester.DownloadTarget{}, err
	}

	return tester.DownloadTarget{
		URL:            target.URL,
		Timeout:        timeout,
		Headers:        httpclient.MergeHeaders(cfg.Download.Headers, cmd.Headers, target.Headers),
		FollowRedirect: firstBool(target.FollowRedirect, cmd.FollowRedirect, boolValuePtr(cfg.Download.FollowRedirect)),
		Rounds:         rounds,
		MaxBytes:       maxBytes,
	}, nil
}

func delayConfigTargets(targets []config.DelayURL) []DelayTargetCommand {
	commands := make([]DelayTargetCommand, 0, len(targets))
	for _, target := range targets {
		commands = append(commands, DelayTargetCommand{
			URL:            target.URL,
			Timeout:        positiveIntValue(target.Timeout),
			Headers:        target.Headers,
			FollowRedirect: target.FollowRedirect,
			Expected:       nonEmptyStringValue(target.Expected),
			Rounds:         positiveIntValue(target.Rounds),
			Unified:        target.Unified,
		})
	}
	return commands
}

func downloadConfigTargets(targets []config.DownloadURL) []DownloadTargetCommand {
	commands := make([]DownloadTargetCommand, 0, len(targets))
	for _, target := range targets {
		commands = append(commands, DownloadTargetCommand{
			URL:            target.URL,
			Timeout:        positiveIntValue(target.Timeout),
			Headers:        target.Headers,
			FollowRedirect: target.FollowRedirect,
			Rounds:         positiveIntValue(target.Rounds),
			MaxBytes:       positiveIntValue(target.MaxBytes),
		})
	}
	return commands
}

func firstInt(name string, values ...*int) (int, error) {
	var selected *int
	for _, value := range values {
		if value == nil {
			continue
		}
		if *value <= 0 {
			return 0, fmt.Errorf("%s must be greater than 0", name)
		}
		if selected == nil {
			selected = value
		}
	}
	if selected == nil {
		return 0, fmt.Errorf("%s must be greater than 0", name)
	}
	return *selected, nil
}

func firstString(name string, values ...*string) (string, error) {
	for _, value := range values {
		if value == nil {
			continue
		}
		if *value == "" {
			return "", fmt.Errorf("%s cannot be empty", name)
		}
		return *value, nil
	}
	return "", fmt.Errorf("%s cannot be empty", name)
}

func firstBool(values ...*bool) bool {
	for _, value := range values {
		if value != nil {
			return *value
		}
	}
	return false
}

func intValue(value int) *int {
	return &value
}

func stringValue(value string) *string {
	return &value
}

func boolValuePtr(value bool) *bool {
	return &value
}

func positiveIntValue(value int) *int {
	if value <= 0 {
		return nil
	}
	return intValue(value)
}

func nonEmptyStringValue(value string) *string {
	if value == "" {
		return nil
	}
	return stringValue(value)
}
