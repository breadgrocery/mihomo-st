package app

import "errors"

var ErrProxyNotFound = errors.New("proxy not found")

var ErrProxySourceRequired = errors.New("payload is required")

var ErrProxyRequestTimeout = errors.New("proxy request timeout")
