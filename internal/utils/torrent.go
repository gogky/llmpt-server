package utils

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/zeebo/bencode"
)

// TorrentMeta is a simplified structure to parse torrent data and calculate hashes
type TorrentMeta struct {
	InfoHash    string
	PieceLength int64
	TotalSize   int64
	FileCount   int
	Files       []FileInfo
}

type FileInfo struct {
	Path string
	Size int64
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

	// Calculate InfoHash
	hashBytes := sha1.Sum(meta.Info)
	infoHash := hex.EncodeToString(hashBytes[:])

	// Decode info dictionary to get files and piece_length
	var infoDict struct {
		PieceLength int64 `bencode:"piece length"`
		Files       []struct {
			Length int64    `bencode:"length"`
			Path   []string `bencode:"path"`
		} `bencode:"files"`
		// For single-file torrents (rare in this usecase, but handled for completeness)
		Name   string `bencode:"name"`
		Length int64  `bencode:"length"`
	}

	if err := bencode.DecodeBytes(meta.Info, &infoDict); err != nil {
		return nil, fmt.Errorf("failed to decode info dict: %w", err)
	}

	res := &TorrentMeta{
		InfoHash:    infoHash,
		PieceLength: infoDict.PieceLength,
		Files:       make([]FileInfo, 0),
	}

	// Handle multi-file torrent
	if len(infoDict.Files) > 0 {
		for _, f := range infoDict.Files {
			// skip pad files
			pathStr := ""
			for i, p := range f.Path {
				if i > 0 {
					pathStr += "/"
				}
				pathStr += p
			}
			res.Files = append(res.Files, FileInfo{Path: pathStr, Size: f.Length})
			res.TotalSize += f.Length
			res.FileCount++
		}
	} else if infoDict.Length > 0 {
		// Handle single-file torrent
		res.Files = append(res.Files, FileInfo{Path: infoDict.Name, Size: infoDict.Length})
		res.TotalSize += infoDict.Length
		res.FileCount = 1
	} else {
		return nil, errors.New("torrent contains no files")
	}

	return res, nil
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
