package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

func decodeRequiredJSON(r *http.Request, dst any) error {
	return decodeJSONBody(r, dst, false)
}

func decodeOptionalJSON(r *http.Request, dst any) error {
	return decodeJSONBody(r, dst, true)
}

func decodeJSONBody(r *http.Request, dst any, optional bool) error {
	defer r.Body.Close()
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(buf)) == 0 {
		if optional {
			return nil
		}
		return io.EOF
	}

	var raw any
	rawDecoder := json.NewDecoder(bytes.NewReader(buf))
	rawDecoder.UseNumber()
	if err := rawDecoder.Decode(&raw); err != nil {
		return err
	}
	if err := ensureSingleJSONValue(rawDecoder); err != nil {
		return err
	}
	if err := rejectJSONNulls(raw, "request"); err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(buf))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	return ensureSingleJSONValue(decoder)
}

func rejectJSONNulls(value any, path string) error {
	if value == nil {
		return fmt.Errorf("%s cannot be null", path)
	}
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if err := rejectJSONNulls(item, joinRequestPath(path, key)); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range v {
			if err := rejectJSONNulls(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func joinRequestPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain only one JSON value")
	}
	return nil
}

func readRequiredJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := decodeRequiredJSON(r, dst); err != nil {
		writeBadRequest(w, err)
		return false
	}
	return true
}

func readOptionalJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := decodeOptionalJSON(r, dst); err != nil {
		writeBadRequest(w, err)
		return false
	}
	return true
}
