package api

import (
	"bytes"
	"encoding/hex"
	"testing"

	"llmpt/internal/models"

	"github.com/zeebo/bencode"
)

func buildPureV2TorrentBytes(t *testing.T) ([]byte, string, string) {
	t.Helper()

	configRoot := bytes.Repeat([]byte{0xaa}, 32)
	modelRoot := bytes.Repeat([]byte{0xbb}, 32)
	info := map[string]any{
		"meta version": int64(2),
		"name":         "repo",
		"piece length": int64(262144),
		"file tree": map[string]any{
			"config.json": map[string]any{
				"": map[string]any{
					"length":      int64(14),
					"pieces root": configRoot,
				},
			},
			"model.bin": map[string]any{
				"": map[string]any{
					"length":      int64(1000),
					"pieces root": modelRoot,
				},
			},
		},
	}

	infoBytes, err := bencode.EncodeBytes(info)
	if err != nil {
		t.Fatalf("encode info: %v", err)
	}

	torrentBytes, err := bencode.EncodeBytes(struct {
		Info bencode.RawMessage `bencode:"info"`
	}{Info: infoBytes})
	if err != nil {
		t.Fatalf("encode torrent: %v", err)
	}

	return torrentBytes, hex.EncodeToString(configRoot), hex.EncodeToString(modelRoot)
}

func TestHydrateTorrentFileRoots(t *testing.T) {
	torrentBytes, configRoot, modelRoot := buildPureV2TorrentBytes(t)

	torrent := models.Torrent{
		TorrentData: torrentBytes,
		Files: []models.TorrentFile{
			{Path: "config.json", Size: 14},
			{Path: "model.bin", Size: 1000},
		},
	}

	changed, err := hydrateTorrentFileRoots(&torrent)
	if err != nil {
		t.Fatalf("hydrateTorrentFileRoots() error = %v", err)
	}
	if !changed {
		t.Fatalf("hydrateTorrentFileRoots() changed = false, want true")
	}

	if torrent.Files[0].FileRoot != configRoot {
		t.Fatalf("config.json file_root = %q, want %q", torrent.Files[0].FileRoot, configRoot)
	}
	if torrent.Files[1].FileRoot != modelRoot {
		t.Fatalf("model.bin file_root = %q, want %q", torrent.Files[1].FileRoot, modelRoot)
	}
}

func TestMatchingTorrentFiles(t *testing.T) {
	torrent := models.Torrent{
		Files: []models.TorrentFile{
			{Path: "config.json", Size: 14, FileRoot: "aaa"},
			{Path: "model.bin", Size: 1000, FileRoot: "bbb"},
			{Path: "copy/model.bin", Size: 1000, FileRoot: "bbb"},
			{Path: "wrong-size.bin", Size: 1001, FileRoot: "bbb"},
		},
	}

	matches := matchingTorrentFiles(torrent, "bbb", 1000)
	if len(matches) != 2 {
		t.Fatalf("len(matches) = %d, want 2", len(matches))
	}
	if matches[0].Path != "model.bin" {
		t.Fatalf("matches[0].Path = %q, want model.bin", matches[0].Path)
	}
	if matches[1].Path != "copy/model.bin" {
		t.Fatalf("matches[1].Path = %q, want copy/model.bin", matches[1].Path)
	}
}

func TestSortFileSourceCandidates(t *testing.T) {
	target := fileSourceTarget{
		RepoID:   "demo/repo",
		Revision: "main",
		RepoType: "model",
		Path:     "model.bin",
	}
	candidates := []fileSourceCandidate{
		{RepoID: "demo/repo", Revision: "oldrev", RepoType: "model", Path: "model.bin", Seeders: 4},
		{RepoID: "demo/repo", Revision: "main", RepoType: "model", Path: "model.bin", Seeders: 0},
		{RepoID: "demo/repo", Revision: "older", RepoType: "model", Path: "model.bin", Seeders: 9},
	}

	sortFileSourceCandidates(candidates, target)

	if candidates[0].Revision != "main" {
		t.Fatalf("candidates[0].Revision = %q, want main", candidates[0].Revision)
	}
	if candidates[1].Revision != "older" {
		t.Fatalf("candidates[1].Revision = %q, want older", candidates[1].Revision)
	}
	if candidates[2].Revision != "oldrev" {
		t.Fatalf("candidates[2].Revision = %q, want oldrev", candidates[2].Revision)
	}
}
