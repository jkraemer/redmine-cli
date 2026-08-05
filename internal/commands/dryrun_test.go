package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderDryRun_NilBody_JSON(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, format: "json"}
	if err := renderDryRun(rc, "DELETE", "/issues/1/watchers/2.json", nil, nil); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, present := got["body"]; present {
		t.Errorf("body key must be omitted for nil body: %v", got)
	}
	if got["method"] != "DELETE" {
		t.Errorf("method=%v", got["method"])
	}
}

func TestRenderDryRun_NilBody_Markdown(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, format: "markdown"}
	if err := renderDryRun(rc, "DELETE", "/issues/1/watchers/2.json", nil, nil); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "```") || strings.Contains(s, "null") {
		t.Errorf("markdown must omit the body block for nil body:\n%s", s)
	}
	if !strings.Contains(s, "would DELETE /issues/1/watchers/2.json") {
		t.Errorf("missing action line:\n%s", s)
	}
}
