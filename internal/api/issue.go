package api

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// issueFields has Issue's fields without its JSON methods, so the methods
// can use the default struct encoding for the standard fields.
type issueFields Issue

// issueJSONKeys are the JSON keys of Issue's typed fields; any other key of
// a server issue object goes to Issue.Extra.
var issueJSONKeys = func() map[string]bool {
	keys := map[string]bool{}
	t := reflect.TypeOf(issueFields{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}()

// UnmarshalJSON decodes the standard fields into Issue's typed fields and
// keeps every other top-level key, unchanged, in Extra.
func (is *Issue) UnmarshalJSON(b []byte) error {
	var f issueFields
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	for k := range all {
		if issueJSONKeys[k] {
			delete(all, k)
		}
	}
	f.Extra = nil
	if len(all) > 0 {
		f.Extra = all
	}
	*is = Issue(f)
	return nil
}

// MarshalJSON encodes the standard fields followed by the Extra keys in
// sorted order. HTML escaping is off, matching the CLI's JSON output.
func (is Issue) MarshalJSON() ([]byte, error) {
	known, err := marshalNoEscape(issueFields(is))
	if err != nil || len(is.Extra) == 0 {
		return known, err
	}
	keys := make([]string, 0, len(is.Extra))
	for k := range is.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// known is a JSON object with at least the always-present id field, so
	// every extra key is appended after a comma.
	buf := bytes.NewBuffer(known[:len(known)-1])
	for _, k := range keys {
		name, err := marshalNoEscape(k)
		if err != nil {
			return nil, err
		}
		buf.WriteByte(',')
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(is.Extra[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshalNoEscape is json.Marshal without HTML escaping and without the
// encoder's trailing newline.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
