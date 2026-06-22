package server

import "fmt"

func requiredString(name string, value *string) (string, error) {
	if value == nil || *value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return *value, nil
}

func optionalPositiveInt(name string, value *int) (*int, error) {
	if value == nil {
		return nil, nil
	}
	if *value <= 0 {
		return nil, fmt.Errorf("%s must be greater than 0", name)
	}
	v := *value
	return &v, nil
}

func optionalNonEmptyString(name string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if *value == "" {
		return nil, fmt.Errorf("%s cannot be empty", name)
	}
	v := *value
	return &v, nil
}

func cloneHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	clone := make(map[string]string, len(headers))
	for key, value := range headers {
		clone[key] = value
	}
	return clone
}
