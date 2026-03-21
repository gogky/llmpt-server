package api

import (
	"context"
	"log"

	"llmpt/internal/models"
	"llmpt/internal/utils"

	"go.mongodb.org/mongo-driver/bson"
)

func activeTorrentFilter() bson.M {
	return bson.M{
		"$or": []bson.M{
			{"status": "active"},
			{"status": bson.M{"$exists": false}},
		},
	}
}

func mergeTorrentFilter(extra bson.M) bson.M {
	filter := activeTorrentFilter()
	for key, value := range extra {
		filter[key] = value
	}
	return filter
}

func swarmKeyForTorrent(torrent models.Torrent) string {
	if torrent.AnnounceKey != "" {
		return torrent.AnnounceKey
	}
	return torrent.InfoHash
}

func hydrateTorrentFileRoots(torrent *models.Torrent) (bool, error) {
	if torrent == nil || len(torrent.TorrentData) == 0 || len(torrent.Files) == 0 {
		return false, nil
	}

	meta, err := utils.ParseTorrent(torrent.TorrentData)
	if err != nil {
		return false, err
	}

	parsedByPath := make(map[string]utils.FileInfo, len(meta.Files))
	for _, file := range meta.Files {
		parsedByPath[file.Path] = file
	}

	changed := false
	for i := range torrent.Files {
		parsed, ok := parsedByPath[torrent.Files[i].Path]
		if !ok || parsed.Size != torrent.Files[i].Size {
			continue
		}
		if torrent.Files[i].FileRoot == parsed.FileRoot {
			continue
		}
		torrent.Files[i].FileRoot = parsed.FileRoot
		changed = true
	}

	return changed, nil
}

func findTorrentFileByPath(torrent models.Torrent, path string) (models.TorrentFile, bool) {
	for _, file := range torrent.Files {
		if file.Path == path {
			return file, true
		}
	}
	return models.TorrentFile{}, false
}

func matchingTorrentFiles(torrent models.Torrent, fileRoot string, size int64) []models.TorrentFile {
	if fileRoot == "" || size < 0 {
		return []models.TorrentFile{}
	}

	matches := make([]models.TorrentFile, 0)
	for _, file := range torrent.Files {
		if file.FileRoot == fileRoot && file.Size == size {
			matches = append(matches, file)
		}
	}
	return matches
}

func (h *Handler) persistTorrentFiles(ctx context.Context, torrent models.Torrent) {
	if h == nil || h.db == nil || h.db.MongoDB == nil || torrent.ID.IsZero() {
		return
	}

	_, err := h.db.MongoDB.TorrentsCollection().UpdateByID(
		ctx,
		torrent.ID,
		bson.M{"$set": bson.M{"files": torrent.Files}},
	)
	if err != nil {
		log.Printf("Failed to backfill file_root for torrent %s: %v", torrent.ID.Hex(), err)
	}
}

func (h *Handler) getSeederCount(ctx context.Context, torrent models.Torrent) int64 {
	if h == nil || h.db == nil || h.db.Redis == nil {
		return 0
	}

	seeders, _, err := h.db.Redis.GetPeerCount(ctx, swarmKeyForTorrent(torrent))
	if err != nil {
		log.Printf("Failed to get peer count for %s: %v", swarmKeyForTorrent(torrent), err)
		return 0
	}
	return seeders
}
