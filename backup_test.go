package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateBackup(t *testing.T) {
	good := Backup{
		Nodes: []Node{{ID: "a", Body: "one"}, {ID: "b", Body: "two", Status: statusRetired}},
		Edges: []Edge{{ID: 1, Src: "a", Dst: "b"}},
	}
	if err := validateBackup(good); err != nil {
		t.Fatalf("valid backup rejected: %v", err)
	}

	bad := map[string]Backup{
		"empty":             {},
		"node without id":   {Nodes: []Node{{Body: "x"}}},
		"node without body": {Nodes: []Node{{ID: "a", Body: "  "}}},
		"duplicate id":      {Nodes: []Node{{ID: "a", Body: "x"}, {ID: "a", Body: "y"}}},
		"unknown status":    {Nodes: []Node{{ID: "a", Body: "x", Status: "deleted"}}},
		"dangling edge":     {Nodes: []Node{{ID: "a", Body: "x"}}, Edges: []Edge{{ID: 1, Src: "a", Dst: "missing"}}},
	}
	for name, b := range bad {
		if err := validateBackup(b); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestNormalizeMetadataTextAlwaysYieldsAnObject(t *testing.T) {
	cases := map[string]string{
		"":              "{}",
		"   ":           "{}",
		`{"a":1}`:       `{"a":1}`,
		"null":          `{"legacy_metadata":"null"}`,
		`[1,2]`:         `{"legacy_metadata":"[1,2]"}`,
		`"text"`:        `{"legacy_metadata":"\"text\""}`,
		"not json at x": `{"legacy_metadata":"not json at x"}`,
	}
	for in, want := range cases {
		if got := normalizeMetadataText(in); got != want {
			t.Errorf("normalizeMetadataText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMetadataObject(t *testing.T) {
	if got := metadataObject(nil); got != "{}" {
		t.Fatalf("nil: got %q", got)
	}
	if got := metadataObject(map[string]any{"k": "v"}); got != `{"k":"v"}` {
		t.Fatalf("object: got %q", got)
	}
	if got := metadataObject("plain"); got != `{"value":"plain"}` {
		t.Fatalf("scalar must be wrapped, got %q", got)
	}
	if got := metadataObject([]any{1.0}); got != `{"value":[1]}` {
		t.Fatalf("array must be wrapped, got %q", got)
	}
}

func TestNodeRowsFromBackupNormalizesMetadata(t *testing.T) {
	rows := nodeRowsFromBackup([]Node{{ID: "a", Body: "x", Metadata: "[1]"}, {ID: "b", Body: "y"}})
	if rows[0][7] != `{"legacy_metadata":"[1]"}` || rows[1][7] != "{}" {
		t.Fatalf("metadata not normalized: %v / %v", rows[0][7], rows[1][7])
	}
}

func TestBackupEncodeDecodeRoundTrip(t *testing.T) {
	in := Backup{
		Version: backupVersion,
		Nodes: []Node{
			{ID: "a", Body: "kept", Source: "system", Status: statusActive, FactKey: "system:host:hostname"},
			{ID: "b", Body: "retired", Source: "agent", Status: statusRetired},
		},
		Edges: []Edge{{ID: 7, Src: "a", Dst: "b", Rel: "related", Weight: 1}},
	}
	for _, gz := range []bool{false, true} {
		var buf bytes.Buffer
		if err := encodeBackup(&buf, in, gz); err != nil {
			t.Fatalf("gzip=%v encode: %v", gz, err)
		}
		out, err := decodeBackup(&buf, gz)
		if err != nil {
			t.Fatalf("gzip=%v decode: %v", gz, err)
		}
		if len(out.Nodes) != 2 || out.Nodes[0].FactKey != "system:host:hostname" || out.Nodes[1].Status != statusRetired {
			t.Fatalf("gzip=%v: steward columns lost in round trip: %+v", gz, out.Nodes)
		}
		if len(out.Edges) != 1 || out.Edges[0].ID != 7 {
			t.Fatalf("gzip=%v: edges lost in round trip: %+v", gz, out.Edges)
		}
	}
}

func TestDecodeBackupRejectsGarbageAndOversize(t *testing.T) {
	if _, err := decodeBackup(strings.NewReader("not gzip"), true); err == nil {
		t.Fatal("invalid gzip must be rejected")
	}
	if _, err := decodeBackup(strings.NewReader("{broken"), false); err == nil {
		t.Fatal("invalid JSON must be rejected")
	}
	c := &capReader{r: strings.NewReader("abcdef"), left: 3}
	buf := make([]byte, 16)
	n, _ := c.Read(buf)
	if n != 3 {
		t.Fatalf("capReader returned %d bytes, want 3", n)
	}
	if _, err := c.Read(buf); err == nil {
		t.Fatal("capReader must error once the cap is exhausted")
	}
}
