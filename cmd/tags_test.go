package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tagFixture = "# 2026-06-01\n\n" +
	"## 09:00 #foo #bar\nnote about foo and bar\n\n" +
	"## 10:00 #foo\nanother foo note\n\n" +
	"## 11:00 #baz\njust baz\n"

func TestListTagsEmpty(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": "# 2026-06-01\n\n## 09:00\nno tags here\n",
	})
	tags, err := listTags(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Errorf("expected no tags, got %v", tags)
	}
}

func TestListTagsWithCounts(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": tagFixture,
	})
	tags, err := listTags(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, tc := range tags {
		counts[tc.Tag] = tc.Count
	}
	if counts["foo"] != 2 {
		t.Errorf("foo count = %d, want 2", counts["foo"])
	}
	if counts["bar"] != 1 {
		t.Errorf("bar count = %d, want 1", counts["bar"])
	}
	if counts["baz"] != 1 {
		t.Errorf("baz count = %d, want 1", counts["baz"])
	}
}

func TestListTagsJSON(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": tagFixture,
	})
	tags, err := listTags(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := renderTagList(&buf, tags, true); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Tags []struct {
			Tag   string `json:"tag"`
			Count int    `json:"count"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(env.Tags) == 0 {
		t.Fatal("expected non-empty tags array in JSON")
	}
	for _, item := range env.Tags {
		if item.Tag == "" {
			t.Error("JSON tag item has empty tag")
		}
		if item.Count <= 0 {
			t.Errorf("JSON tag item %q has count %d", item.Tag, item.Count)
		}
	}
}

func TestListTagsJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTagList(&buf, nil, true); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Tags []any `json:"tags"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON for empty tags: %v\n%s", err, buf.String())
	}
	if env.Tags == nil {
		t.Error("empty tags should produce [] not null")
	}
}

func TestRenameTagsRewritesFiles(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": tagFixture,
	})
	ctx := context.Background()

	n, err := renameTags(ctx, cfg, fake, "foo", "qux", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("renamed %d files, want 1", n)
	}

	data, _ := os.ReadFile(filepath.Join(cfg.Root(), "daily", "2026", "06", "2026-06-01.md"))
	got := string(data)
	if strings.Contains(got, "#foo") {
		t.Errorf("#foo still present after rename:\n%s", got)
	}
	if !strings.Contains(got, "#qux") {
		t.Errorf("#qux not present after rename:\n%s", got)
	}
	// Unrelated tags should be untouched.
	if !strings.Contains(got, "#bar") {
		t.Errorf("#bar was disturbed:\n%s", got)
	}
	if !strings.Contains(got, "#baz") {
		t.Errorf("#baz was disturbed:\n%s", got)
	}

	// Store reflects the rename: foo gone, qux present.
	tags, err := listTags(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tagNames := map[string]bool{}
	for _, tc := range tags {
		tagNames[tc.Tag] = true
	}
	if tagNames["foo"] {
		t.Error("foo still in index after rename")
	}
	if !tagNames["qux"] {
		t.Error("qux not in index after rename")
	}
}

func TestRenameTagsDryRun(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": tagFixture,
	})
	original, _ := os.ReadFile(filepath.Join(cfg.Root(), "daily", "2026", "06", "2026-06-01.md"))

	n, err := renameTags(context.Background(), cfg, fake, "foo", "qux", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("dry-run reported %d files, want 1", n)
	}

	// File must be unchanged.
	after, _ := os.ReadFile(filepath.Join(cfg.Root(), "daily", "2026", "06", "2026-06-01.md"))
	if string(after) != string(original) {
		t.Errorf("dry-run modified the file:\nbefore: %s\nafter:  %s", original, after)
	}
}

func TestRenameTagsNoMatches(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": tagFixture,
	})
	n, err := renameTags(context.Background(), cfg, fake, "notexist", "other", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 changed files, got %d", n)
	}
}

func TestRenameTagsValidation(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": tagFixture,
	})
	ctx := context.Background()

	if _, err := renameTags(ctx, cfg, fake, "", "new", false, nil); err == nil {
		t.Error("expected error for empty old tag")
	}
	if _, err := renameTags(ctx, cfg, fake, "old", "", false, nil); err == nil {
		t.Error("expected error for empty new tag")
	}
	if _, err := renameTags(ctx, cfg, fake, "has space", "new", false, nil); err == nil {
		t.Error("expected error for invalid old tag")
	}
	if _, err := renameTags(ctx, cfg, fake, "old", "has space", false, nil); err == nil {
		t.Error("expected error for invalid new tag")
	}
	if _, err := renameTags(ctx, cfg, fake, "same", "same", false, nil); err == nil {
		t.Error("expected error when old == new")
	}
}

func TestBuildTagReplaceRe(t *testing.T) {
	cases := []struct {
		name    string
		tag     string
		input   string
		want    string
		changed bool
	}{
		{"inline space", "foo", "text #foo bar", "text #new bar", true},
		{"at line end", "foo", "text #foo", "text #new", true},
		{"at file start", "foo", "#foo bar", "#new bar", true},
		{"after newline", "foo", "line1\n#foo end", "line1\n#new end", true},
		{"prefix longer tag", "foo", "text #foobar", "text #foobar", false},
		{"hyphenated suffix", "foo", "text #foo-bar", "text #foo-bar", false},
		{"exact hyphenated rename", "foo-bar", "text #foo-bar end", "text #new end", true},
		{"multiple occurrences", "foo", "a #foo b #foo c", "a #new b #new c", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			re := buildTagReplaceRe(tc.tag)
			got := re.ReplaceAllString(tc.input, "${1}#new${3}")
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if (got != tc.input) != tc.changed {
				t.Errorf("changed=%v but got %q from %q", tc.changed, got, tc.input)
			}
		})
	}
}
