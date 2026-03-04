package api

import (
	"log"
	"net/http"

	"llmpt/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// DownloadTorrent 返回原始 .torrent 文件二进制数据
// GET /api/v1/torrents/torrent?repo_id=X&revision=Y
func (h *Handler) DownloadTorrent(w http.ResponseWriter, r *http.Request) {
	repoID := r.URL.Query().Get("repo_id")
	revision := r.URL.Query().Get("revision")

	if repoID == "" || revision == "" {
		ErrorRes(w, http.StatusBadRequest, "repo_id and revision query parameters are required")
		return
	}

	ctx := r.Context()
	collection := h.db.MongoDB.TorrentsCollection()

	filter := bson.M{"repo_id": repoID, "revision": revision}
	// Only project the torrent_data field to avoid loading the entire document
	opts := options.FindOne().SetProjection(bson.M{"torrent_data": 1})

	var result models.Torrent
	if err := collection.FindOne(ctx, filter, opts).Decode(&result); err != nil {
		log.Printf("Torrent not found for %s@%s: %v", repoID, revision, err)
		ErrorRes(w, http.StatusNotFound, "torrent not found")
		return
	}

	if len(result.TorrentData) == 0 {
		ErrorRes(w, http.StatusNotFound, "torrent data not available")
		return
	}

	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", "attachment; filename=\"torrent.torrent\"")
	w.WriteHeader(http.StatusOK)
	w.Write(result.TorrentData)
}
