package api

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// fullIssueJSON mirrors Redmine's issues/show.api.rsb with every include,
// plus two keys added by plugins (customer_company, planio_score).
const fullIssueJSON = `{
  "id": 7,
  "project": {"id": 1, "name": "P"},
  "tracker": {"id": 2, "name": "Bug"},
  "status": {"id": 3, "name": "Closed", "is_closed": true},
  "priority": {"id": 4, "name": "Normal"},
  "author": {"id": 5, "name": "Ann"},
  "assigned_to": {"id": 6, "name": "Bob"},
  "category": {"id": 7, "name": "UI"},
  "fixed_version": {"id": 8, "name": "1.0"},
  "parent": {"id": 3},
  "subject": "Subj <b>",
  "description": "Body",
  "start_date": "2026-01-01",
  "due_date": "2026-02-01",
  "done_ratio": 0,
  "is_private": false,
  "estimated_hours": 2.5,
  "total_estimated_hours": 4.0,
  "spent_hours": 0.0,
  "total_spent_hours": 1.5,
  "custom_fields": [{"id": 1, "name": "Tags", "multiple": true, "value": ["a", "b"]}],
  "created_on": "2026-01-01T10:00:00Z",
  "updated_on": "2026-01-02T10:00:00Z",
  "closed_on": "2026-01-03T10:00:00Z",
  "children": [
    {"id": 10, "tracker": {"id": 2, "name": "Bug"}, "subject": "Child",
     "children": [{"id": 11, "tracker": {"id": 2, "name": "Bug"}, "subject": "Grandchild"}]}
  ],
  "attachments": [{"id": 42, "filename": "a.png", "filesize": 10, "content_type": "image/png",
    "description": "", "content_url": "https://x/a.png", "thumbnail_url": "https://x/t.png",
    "author": {"id": 5, "name": "Ann"}, "created_on": "2026-01-01T10:00:00Z"}],
  "relations": [{"id": 1, "issue_id": 7, "issue_to_id": 8, "relation_type": "precedes", "delay": 2}],
  "changesets": [{"revision": "abc123", "user": {"id": 5, "name": "Ann"}, "comments": "fix", "committed_on": "2026-01-01T10:00:00Z"}],
  "journals": [{"id": 1, "user": {"id": 5, "name": "Ann"}, "notes": "n", "created_on": "2026-01-01T10:00:00Z",
    "updated_on": "2026-01-01T11:00:00Z", "updated_by": {"id": 6, "name": "Bob"}, "private_notes": true,
    "details": [{"property": "attr", "name": "status_id", "old_value": "1", "new_value": "3"}]}],
  "watchers": [{"id": 6, "name": "Bob"}],
  "allowed_statuses": [{"id": 1, "name": "New", "is_closed": false}],
  "customer_company": {"id": 9, "name": "ACME"},
  "planio_score": 42
}`

func decodeFullIssue(t *testing.T) Issue {
	t.Helper()
	var is Issue
	if err := json.Unmarshal([]byte(fullIssueJSON), &is); err != nil {
		t.Fatal(err)
	}
	return is
}

func TestIssue_StandardFieldsAreTyped(t *testing.T) {
	is := decodeFullIssue(t)

	if is.FixedVersion == nil || is.FixedVersion.Name != "1.0" {
		t.Errorf("FixedVersion=%+v", is.FixedVersion)
	}
	if is.Parent == nil || is.Parent.ID != 3 {
		t.Errorf("Parent=%+v", is.Parent)
	}
	if is.Status.IsClosed == nil || !*is.Status.IsClosed {
		t.Errorf("Status.IsClosed=%v", is.Status.IsClosed)
	}
	if is.EstimatedHours == nil || *is.EstimatedHours != 2.5 {
		t.Errorf("EstimatedHours=%v", is.EstimatedHours)
	}
	if is.SpentHours == nil || *is.SpentHours != 0 {
		t.Errorf("SpentHours=%v (0.0 must be kept, not treated as unset)", is.SpentHours)
	}
	if is.ClosedOn != "2026-01-03T10:00:00Z" {
		t.Errorf("ClosedOn=%q", is.ClosedOn)
	}
	if len(is.Children) != 1 || len(is.Children[0].Children) != 1 || is.Children[0].Children[0].Subject != "Grandchild" {
		t.Errorf("Children=%+v", is.Children)
	}
	if len(is.Relations) != 1 || is.Relations[0].RelationType != "precedes" || is.Relations[0].Delay == nil || *is.Relations[0].Delay != 2 {
		t.Errorf("Relations=%+v", is.Relations)
	}
	if len(is.Changesets) != 1 || is.Changesets[0].Revision != "abc123" {
		t.Errorf("Changesets=%+v", is.Changesets)
	}
	if len(is.AllowedStatuses) != 1 || is.AllowedStatuses[0].Name != "New" {
		t.Errorf("AllowedStatuses=%+v", is.AllowedStatuses)
	}
	j := is.Journals[0]
	if !j.PrivateNotes || j.UpdatedBy == nil || j.UpdatedBy.Name != "Bob" || j.UpdatedOn == "" {
		t.Errorf("Journal=%+v", j)
	}
	if is.Attachments[0].ThumbnailURL != "https://x/t.png" {
		t.Errorf("ThumbnailURL=%q", is.Attachments[0].ThumbnailURL)
	}
	if !is.CustomFields[0].Multiple {
		t.Errorf("CustomFields[0].Multiple=false")
	}
}

func TestIssue_OnlyNonStandardKeysGoToExtra(t *testing.T) {
	is := decodeFullIssue(t)

	var keys []string
	for k := range is.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if want := []string{"customer_company", "planio_score"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("Extra keys=%v want %v", keys, want)
	}
}

func TestIssue_NoExtraKeysLeavesExtraNil(t *testing.T) {
	var is Issue
	if err := json.Unmarshal([]byte(`{"id":1,"subject":"s"}`), &is); err != nil {
		t.Fatal(err)
	}
	if is.Extra != nil {
		t.Errorf("Extra=%v want nil", is.Extra)
	}
}

// stripEmpty drops null and "" values, which the typed fields omit; every
// other value the server sent must survive a decode/encode round trip.
func stripEmpty(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			if e == nil || e == "" {
				continue
			}
			out[k] = stripEmpty(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = stripEmpty(e)
		}
		return out
	}
	return v
}

func encodeNoEscape(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestIssue_RoundTripKeepsEverything(t *testing.T) {
	is := decodeFullIssue(t)
	got := encodeNoEscape(t, map[string]any{"issue": &is})

	var want, have any
	if err := json.Unmarshal([]byte(`{"issue":`+fullIssueJSON+`}`), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &have); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, got)
	}
	if !reflect.DeepEqual(stripEmpty(want), stripEmpty(have)) {
		t.Errorf("round trip lost or changed data:\n%s", got)
	}
}

func TestIssue_MarshalDoesNotEscapeHTML(t *testing.T) {
	is := decodeFullIssue(t)
	got := string(encodeNoEscape(t, is))
	if !strings.Contains(got, `"subject":"Subj <b>"`) {
		t.Errorf("subject HTML-escaped or missing:\n%s", got)
	}
}

func TestIssue_MarshalPutsExtraAfterStandardFields(t *testing.T) {
	is := decodeFullIssue(t)
	got := string(encodeNoEscape(t, is))
	if strings.Index(got, `"customer_company"`) < strings.Index(got, `"subject"`) {
		t.Errorf("extra keys must follow the standard fields:\n%s", got)
	}
	if strings.Index(got, `"customer_company"`) > strings.Index(got, `"planio_score"`) {
		t.Errorf("extra keys must be sorted:\n%s", got)
	}
}
