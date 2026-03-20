package api

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestBuildFileSourceLookupFilterStaysWithinRepo(t *testing.T) {
	filter := buildFileSourceLookupFilter("model", "demo/repo", "abc123", 42)

	if got := filter["repo_type"]; got != "model" {
		t.Fatalf("repo_type = %#v, want %q", got, "model")
	}
	if got := filter["repo_id"]; got != "demo/repo" {
		t.Fatalf("repo_id = %#v, want %q", got, "demo/repo")
	}

	files, ok := filter["files"].(bson.M)
	if !ok {
		t.Fatalf("files filter type = %T, want bson.M", filter["files"])
	}
	elemMatch, ok := files["$elemMatch"].(bson.M)
	if !ok {
		t.Fatalf("$elemMatch type = %T, want bson.M", files["$elemMatch"])
	}
	if got := elemMatch["file_root"]; got != "abc123" {
		t.Fatalf("file_root = %#v, want %q", got, "abc123")
	}
	if got := elemMatch["size"]; got != int64(42) {
		t.Fatalf("size = %#v, want %d", got, 42)
	}
}
