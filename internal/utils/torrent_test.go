package utils

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/zeebo/bencode"
)

func TestParseTorrentPureV2(t *testing.T) {
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
			".pad": map[string]any{
				"262130": map[string]any{
					"": map[string]any{
						"length": int64(262130),
					},
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

	meta, err := ParseTorrent(torrentBytes)
	if err != nil {
		t.Fatalf("ParseTorrent() error = %v", err)
	}

	fullHashBytes := sha256.Sum256(infoBytes)
	wantInfoHash := hex.EncodeToString(fullHashBytes[:])
	wantAnnounceKey := hex.EncodeToString(fullHashBytes[:20])

	if meta.InfoHash != wantInfoHash {
		t.Fatalf("InfoHash = %s, want %s", meta.InfoHash, wantInfoHash)
	}
	if meta.AnnounceKey != wantAnnounceKey {
		t.Fatalf("AnnounceKey = %s, want %s", meta.AnnounceKey, wantAnnounceKey)
	}
	if meta.Name != "repo" {
		t.Fatalf("Name = %q, want repo", meta.Name)
	}
	if meta.PieceLength != 262144 {
		t.Fatalf("PieceLength = %d, want 262144", meta.PieceLength)
	}
	if meta.FileCount != 2 {
		t.Fatalf("FileCount = %d, want 2", meta.FileCount)
	}
	if meta.TotalSize != 1014 {
		t.Fatalf("TotalSize = %d, want 1014", meta.TotalSize)
	}
	if len(meta.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(meta.Files))
	}
	if meta.Files[0] != (FileInfo{
		Path:     "config.json",
		Size:     14,
		FileRoot: hex.EncodeToString(configRoot),
	}) {
		t.Fatalf("Files[0] = %+v", meta.Files[0])
	}
	if meta.Files[1] != (FileInfo{
		Path:     "model.bin",
		Size:     1000,
		FileRoot: hex.EncodeToString(modelRoot),
	}) {
		t.Fatalf("Files[1] = %+v", meta.Files[1])
	}
}

func TestParseTorrentV1StillWorks(t *testing.T) {
	info := map[string]any{
		"name":         "repo",
		"piece length": int64(262144),
		"files": []any{
			map[string]any{
				"length": int64(14),
				"path":   []string{"config.json"},
			},
			map[string]any{
				"length": int64(262130),
				"path":   []string{".pad", "262130"},
			},
			map[string]any{
				"length": int64(1000),
				"path":   []string{"model.bin"},
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

	meta, err := ParseTorrent(torrentBytes)
	if err != nil {
		t.Fatalf("ParseTorrent() error = %v", err)
	}

	hashBytes := sha1.Sum(infoBytes)
	wantInfoHash := hex.EncodeToString(hashBytes[:])

	if meta.InfoHash != wantInfoHash {
		t.Fatalf("InfoHash = %s, want %s", meta.InfoHash, wantInfoHash)
	}
	if meta.AnnounceKey != wantInfoHash {
		t.Fatalf("AnnounceKey = %s, want %s", meta.AnnounceKey, wantInfoHash)
	}
	if meta.FileCount != 2 {
		t.Fatalf("FileCount = %d, want 2", meta.FileCount)
	}
	if meta.TotalSize != 1014 {
		t.Fatalf("TotalSize = %d, want 1014", meta.TotalSize)
	}
}
