package server

import (
	"bytes"
	"encoding/json"
	"fmt"

	"mihomo-st/internal/app"
)

type delayRequestDTO struct {
	Timeout        *int              `json:"timeout"`
	Headers        map[string]string `json:"headers"`
	FollowRedirect *bool             `json:"follow-redirect"`
	Expected       *string           `json:"expected"`
	Rounds         *int              `json:"rounds"`
	Unified        *bool             `json:"unified"`
	URLs           []delayTargetDTO  `json:"urls"`
}

type delayCollectionRequestDTO struct {
	Timeout        *int              `json:"timeout"`
	Headers        map[string]string `json:"headers"`
	FollowRedirect *bool             `json:"follow-redirect"`
	Expected       *string           `json:"expected"`
	Rounds         *int              `json:"rounds"`
	Unified        *bool             `json:"unified"`
	URLs           []delayTargetDTO  `json:"urls"`
	Concurrency    *int              `json:"concurrency"`
}

type delayTargetDTO struct {
	URL            string            `json:"url"`
	Timeout        *int              `json:"timeout"`
	Headers        map[string]string `json:"headers"`
	FollowRedirect *bool             `json:"follow-redirect"`
	Expected       *string           `json:"expected"`
	Rounds         *int              `json:"rounds"`
	Unified        *bool             `json:"unified"`
}

type downloadRequestDTO struct {
	Timeout        *int                `json:"timeout"`
	Headers        map[string]string   `json:"headers"`
	FollowRedirect *bool               `json:"follow-redirect"`
	Rounds         *int                `json:"rounds"`
	MaxBytes       *int                `json:"max-bytes"`
	URLs           []downloadTargetDTO `json:"urls"`
}

type downloadCollectionRequestDTO struct {
	Timeout        *int                `json:"timeout"`
	Headers        map[string]string   `json:"headers"`
	FollowRedirect *bool               `json:"follow-redirect"`
	Rounds         *int                `json:"rounds"`
	MaxBytes       *int                `json:"max-bytes"`
	URLs           []downloadTargetDTO `json:"urls"`
	Concurrency    *int                `json:"concurrency"`
}

type downloadTargetDTO struct {
	URL            string            `json:"url"`
	Timeout        *int              `json:"timeout"`
	Headers        map[string]string `json:"headers"`
	FollowRedirect *bool             `json:"follow-redirect"`
	Rounds         *int              `json:"rounds"`
	MaxBytes       *int              `json:"max-bytes"`
}

func (r delayRequestDTO) Command() (app.DelayCommand, error) {
	return delayCommandFromRequest(r.Timeout, r.Headers, r.FollowRedirect, r.Expected, r.Rounds, r.Unified, r.URLs)
}

func (r delayCollectionRequestDTO) Command() (app.DelayCollectionCommand, error) {
	delayCommand, err := delayCommandFromRequest(r.Timeout, r.Headers, r.FollowRedirect, r.Expected, r.Rounds, r.Unified, r.URLs)
	if err != nil {
		return app.DelayCollectionCommand{}, err
	}
	concurrency, err := optionalPositiveInt("concurrency", r.Concurrency)
	if err != nil {
		return app.DelayCollectionCommand{}, err
	}
	return app.DelayCollectionCommand{
		DelayCommand: delayCommand,
		Concurrency:  concurrency,
	}, nil
}

func delayCommandFromRequest(timeout *int, headers map[string]string, followRedirect *bool, expected *string, rounds *int, unified *bool, urls []delayTargetDTO) (app.DelayCommand, error) {
	normalizedTimeout, err := optionalPositiveInt("timeout", timeout)
	if err != nil {
		return app.DelayCommand{}, err
	}
	normalizedExpected, err := optionalNonEmptyString("expected", expected)
	if err != nil {
		return app.DelayCommand{}, err
	}
	normalizedRounds, err := optionalPositiveInt("rounds", rounds)
	if err != nil {
		return app.DelayCommand{}, err
	}
	targets, err := delayTargetCommands(urls)
	if err != nil {
		return app.DelayCommand{}, err
	}
	return app.DelayCommand{
		Timeout:        normalizedTimeout,
		Headers:        cloneHeaders(headers),
		FollowRedirect: followRedirect,
		Expected:       normalizedExpected,
		Rounds:         normalizedRounds,
		Unified:        unified,
		URLs:           targets,
	}, nil
}

func delayTargetCommands(urls []delayTargetDTO) ([]app.DelayTargetCommand, error) {
	if urls == nil {
		return nil, nil
	}
	targets := make([]app.DelayTargetCommand, 0, len(urls))
	for _, url := range urls {
		target, err := url.command()
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (r delayTargetDTO) command() (app.DelayTargetCommand, error) {
	timeout, err := optionalPositiveInt("timeout", r.Timeout)
	if err != nil {
		return app.DelayTargetCommand{}, err
	}
	expected, err := optionalNonEmptyString("expected", r.Expected)
	if err != nil {
		return app.DelayTargetCommand{}, err
	}
	rounds, err := optionalPositiveInt("rounds", r.Rounds)
	if err != nil {
		return app.DelayTargetCommand{}, err
	}
	return app.DelayTargetCommand{
		URL:            r.URL,
		Timeout:        timeout,
		Headers:        cloneHeaders(r.Headers),
		FollowRedirect: r.FollowRedirect,
		Expected:       expected,
		Rounds:         rounds,
		Unified:        r.Unified,
	}, nil
}

func (r *delayTargetDTO) UnmarshalJSON(buf []byte) error {
	var rawURL string
	if err := json.Unmarshal(buf, &rawURL); err == nil {
		r.URL = rawURL
		return nil
	}

	type delayURLObject delayTargetDTO
	var target delayURLObject
	decoder := json.NewDecoder(bytes.NewReader(buf))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&target); err != nil {
		return fmt.Errorf("urls item must be a string or object: %w", err)
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return err
	}
	*r = delayTargetDTO(target)
	return nil
}

func (r downloadRequestDTO) Command() (app.DownloadCommand, error) {
	return downloadCommandFromRequest(r.Timeout, r.Headers, r.FollowRedirect, r.Rounds, r.MaxBytes, r.URLs)
}

func (r downloadCollectionRequestDTO) Command() (app.DownloadCollectionCommand, error) {
	downloadCommand, err := downloadCommandFromRequest(r.Timeout, r.Headers, r.FollowRedirect, r.Rounds, r.MaxBytes, r.URLs)
	if err != nil {
		return app.DownloadCollectionCommand{}, err
	}
	concurrency, err := optionalPositiveInt("concurrency", r.Concurrency)
	if err != nil {
		return app.DownloadCollectionCommand{}, err
	}
	return app.DownloadCollectionCommand{
		DownloadCommand: downloadCommand,
		Concurrency:     concurrency,
	}, nil
}

func downloadCommandFromRequest(timeout *int, headers map[string]string, followRedirect *bool, rounds *int, maxBytes *int, urls []downloadTargetDTO) (app.DownloadCommand, error) {
	normalizedTimeout, err := optionalPositiveInt("timeout", timeout)
	if err != nil {
		return app.DownloadCommand{}, err
	}
	normalizedRounds, err := optionalPositiveInt("rounds", rounds)
	if err != nil {
		return app.DownloadCommand{}, err
	}
	normalizedMaxBytes, err := optionalPositiveInt("max-bytes", maxBytes)
	if err != nil {
		return app.DownloadCommand{}, err
	}
	targets, err := downloadTargetCommands(urls)
	if err != nil {
		return app.DownloadCommand{}, err
	}
	return app.DownloadCommand{
		Timeout:        normalizedTimeout,
		Headers:        cloneHeaders(headers),
		FollowRedirect: followRedirect,
		Rounds:         normalizedRounds,
		MaxBytes:       normalizedMaxBytes,
		URLs:           targets,
	}, nil
}

func downloadTargetCommands(urls []downloadTargetDTO) ([]app.DownloadTargetCommand, error) {
	if urls == nil {
		return nil, nil
	}
	targets := make([]app.DownloadTargetCommand, 0, len(urls))
	for _, url := range urls {
		target, err := url.command()
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (r downloadTargetDTO) command() (app.DownloadTargetCommand, error) {
	timeout, err := optionalPositiveInt("timeout", r.Timeout)
	if err != nil {
		return app.DownloadTargetCommand{}, err
	}
	rounds, err := optionalPositiveInt("rounds", r.Rounds)
	if err != nil {
		return app.DownloadTargetCommand{}, err
	}
	maxBytes, err := optionalPositiveInt("max-bytes", r.MaxBytes)
	if err != nil {
		return app.DownloadTargetCommand{}, err
	}
	return app.DownloadTargetCommand{
		URL:            r.URL,
		Timeout:        timeout,
		Headers:        cloneHeaders(r.Headers),
		FollowRedirect: r.FollowRedirect,
		Rounds:         rounds,
		MaxBytes:       maxBytes,
	}, nil
}

func (r *downloadTargetDTO) UnmarshalJSON(buf []byte) error {
	var rawURL string
	if err := json.Unmarshal(buf, &rawURL); err == nil {
		r.URL = rawURL
		return nil
	}

	type downloadURLObject downloadTargetDTO
	var target downloadURLObject
	decoder := json.NewDecoder(bytes.NewReader(buf))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&target); err != nil {
		return fmt.Errorf("urls item must be a string or object: %w", err)
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return err
	}
	*r = downloadTargetDTO(target)
	return nil
}
