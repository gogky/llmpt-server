package utils

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/zeebo/bencode"
)

// TorrentMeta is a simplified structure to parse torrent data and calculate hashes
type TorrentMeta struct {
	InfoHash    string
	AnnounceKey string
	Name        string
	PieceLength int64
	TotalSize   int64
	FileCount   int
	Files       []FileInfo
}

type FileInfo struct {
	Path     string
	Size     int64
	FileRoot string
}

// ParseTorrent extracts metadata from raw .torrent bytes
func ParseTorrent(torrentData []byte) (*TorrentMeta, error) {
	var meta struct {
		Info bencode.RawMessage `bencode:"info"`
	}

	if err := bencode.DecodeBytes(torrentData, &meta); err != nil {
		return nil, fmt.Errorf("failed to decode bencode: %w", err)
	}

	if len(meta.Info) == 0 {
		return nil, errors.New("missing info section in torrent")
	}

	// Decode info dictionary to get files and piece_length.
	// We support both legacy v1 "files" lists and v2 "file tree".
	var infoDict struct {
		MetaVersion int64 `bencode:"meta version"`
		PieceLength int64 `bencode:"piece length"`
		Files       []struct {
			Length int64    `bencode:"length"`
			Path   []string `bencode:"path"`
		} `bencode:"files"`
		FileTree map[string]bencode.RawMessage `bencode:"file tree"`
		// For single-file torrents (rare in this usecase, but handled for completeness)
		Name   string `bencode:"name"`
		Length int64  `bencode:"length"`
	}

	if err := bencode.DecodeBytes(meta.Info, &infoDict); err != nil {
		return nil, fmt.Errorf("failed to decode info dict: %w", err)
	}

	res := &TorrentMeta{
		Name:        infoDict.Name,
		PieceLength: infoDict.PieceLength,
		Files:       make([]FileInfo, 0),
	}

	switch {
	case infoDict.MetaVersion == 2:
		hashBytes := sha256.Sum256(meta.Info)
		res.InfoHash = hex.EncodeToString(hashBytes[:])
		res.AnnounceKey = hex.EncodeToString(hashBytes[:20])

		if len(infoDict.FileTree) == 0 {
			return nil, errors.New("v2 torrent missing file tree")
		}

		if err := parseV2FileTree(infoDict.FileTree, nil, &res.Files); err != nil {
			return nil, err
		}
	case infoDict.MetaVersion == 0:
		hashBytes := sha1.Sum(meta.Info)
		res.InfoHash = hex.EncodeToString(hashBytes[:])
		res.AnnounceKey = res.InfoHash

		if len(infoDict.Files) > 0 {
			for _, f := range infoDict.Files {
				pathStr := strings.Join(f.Path, "/")
				if isPaddingPath(pathStr) {
					continue
				}
				res.Files = append(res.Files, FileInfo{Path: pathStr, Size: f.Length})
			}
		} else if infoDict.Length > 0 {
			res.Files = append(res.Files, FileInfo{Path: infoDict.Name, Size: infoDict.Length})
		} else {
			return nil, errors.New("torrent contains no files")
		}
	default:
		return nil, fmt.Errorf("unsupported torrent meta version: %d", infoDict.MetaVersion)
	}

	if len(res.Files) == 0 {
		return nil, errors.New("torrent contains no payload files")
	}

	sort.Slice(res.Files, func(i, j int) bool {
		return res.Files[i].Path < res.Files[j].Path
	})

	for _, f := range res.Files {
		res.TotalSize += f.Size
		res.FileCount++
	}

	return res, nil
}

func parseV2FileTree(tree map[string]bencode.RawMessage, prefix []string, files *[]FileInfo) error {
	for name, raw := range tree {
		path := append(append([]string{}, prefix...), name)
		if err := parseV2FileTreeNode(path, raw, files); err != nil {
			return err
		}
	}
	return nil
}

func parseV2FileTreeNode(path []string, raw bencode.RawMessage, files *[]FileInfo) error {
	var node map[string]bencode.RawMessage
	if err := bencode.DecodeBytes(raw, &node); err != nil {
		return fmt.Errorf("failed to decode v2 file tree node %q: %w", strings.Join(path, "/"), err)
	}

	if leafRaw, ok := node[""]; ok {
		var props struct {
			Length     int64  `bencode:"length"`
			PiecesRoot []byte `bencode:"pieces root"`
		}
		if err := bencode.DecodeBytes(leafRaw, &props); err != nil {
			return fmt.Errorf("failed to decode v2 file node %q: %w", strings.Join(path, "/"), err)
		}
		pathStr := strings.Join(path, "/")
		if !isPaddingPath(pathStr) {
			file := FileInfo{Path: pathStr, Size: props.Length}
			if len(props.PiecesRoot) > 0 {
				file.FileRoot = hex.EncodeToString(props.PiecesRoot)
			}
			*files = append(*files, file)
		}
		return nil
	}

	for name, child := range node {
		if name == "" {
			continue
		}
		nextPath := append(append([]string{}, path...), name)
		if err := parseV2FileTreeNode(nextPath, child, files); err != nil {
			return err
		}
	}

	return nil
}

func isPaddingPath(path string) bool {
	normalized := strings.ReplaceAll(path, "\\", "/")
	return normalized == ".pad" || strings.HasPrefix(normalized, ".pad/") || strings.Contains(normalized, "/.pad/")
}

// GetOptimalPieceLength calculates the expected piece length based on the total size
func GetOptimalPieceLength(totalSize int64) int64 {
	const GB = 1024 * 1024 * 1024

	if totalSize < 100*1024*1024 { // <100MB
		return 256 * 1024 // 256KB
	} else if totalSize < 1*GB { // <1GB
		return 1024 * 1024 // 1MB
	} else if totalSize < 10*GB { // <10GB
		return 4 * 1024 * 1024 // 4MB
	} else if totalSize < 100*GB { // <100GB
		return 16 * 1024 * 1024 // 16MB
	} else if totalSize < 1024*GB { // <1TB
		return 32 * 1024 * 1024 // 32MB
	} else { // ≥1TB
		return 64 * 1024 * 1024 // 64MB
	}
}
