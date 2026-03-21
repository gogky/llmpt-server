package api

import (
	"log"
	"net/http"
	"sort"

	"llmpt/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type fileSourceTarget struct {
	RepoID   string `json:"repo_id"`
	Revision string `json:"revision"`
	RepoType string `json:"repo_type"`
	Path     string `json:"path"`
	FileRoot string `json:"file_root,omitempty"`
	Size     int64  `json:"size"`
	Seeders  int64  `json:"seeders"`
}

type fileSourceCandidate struct {
	RepoID      string  `json:"repo_id"`
	Revision    string  `json:"revision"`
	RepoType    string  `json:"repo_type"`
	Path        string  `json:"path"`
	FileRoot    string  `json:"file_root,omitempty"`
	Size        int64   `json:"size"`
	Seeders     int64   `json:"seeders"`
	Score       float64 `json:"score"`
	AnnounceKey string  `json:"announce_key,omitempty"`
}

type fileSourcesResponse struct {
	Target     fileSourceTarget      `json:"target"`
	Candidates []fileSourceCandidate `json:"candidates"`
}

func buildFileSourceLookupFilter(repoType, repoID, fileRoot string, size int64) bson.M {
	return mergeTorrentFilter(bson.M{
		"repo_type": repoType,
		"repo_id":   repoID,
		"files": bson.M{
			"$elemMatch": bson.M{
				"file_root": fileRoot,
				"size":      size,
			},
		},
	})
}

// GetFileSources returns exact-content source candidates for one target file.
// GET /api/v1/file-sources?repo_id=X&revision=Y&path=Z
func (h *Handler) GetFileSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ErrorRes(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	repoID := r.URL.Query().Get("repo_id")
	revision := r.URL.Query().Get("revision")
	path := r.URL.Query().Get("path")
	if repoID == "" || revision == "" || path == "" {
		ErrorRes(w, http.StatusBadRequest, "repo_id, revision, and path query parameters are required")
		return
	}

	repoType := r.URL.Query().Get("repo_type")
	if repoType == "" {
		repoType = "model"
	}

	ctx := r.Context()
	collection := h.db.MongoDB.TorrentsCollection()

	var targetTorrent models.Torrent
	err := collection.FindOne(
		ctx,
		mergeTorrentFilter(bson.M{
			"repo_type": repoType,
			"repo_id":   repoID,
			"revision":  revision,
		}),
	).Decode(&targetTorrent)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			ErrorRes(w, http.StatusNotFound, "target torrent not found")
			return
		}
		log.Printf("Failed to query target torrent for %s/%s@%s: %v", repoType, repoID, revision, err)
		ErrorRes(w, http.StatusInternalServerError, "failed to query target torrent")
		return
	}

	if changed, err := hydrateTorrentFileRoots(&targetTorrent); err != nil {
		log.Printf("Failed to parse target torrent_data for %s@%s: %v", repoID, revision, err)
		ErrorRes(w, http.StatusInternalServerError, "failed to parse target torrent metadata")
		return
	} else if changed {
		h.persistTorrentFiles(ctx, targetTorrent)
	}

	targetFile, ok := findTorrentFileByPath(targetTorrent, path)
	if !ok {
		ErrorRes(w, http.StatusNotFound, "target file not found in torrent")
		return
	}

	seederCache := map[string]int64{}
	getSeederCountCached := func(torrent models.Torrent) int64 {
		swarmKey := swarmKeyForTorrent(torrent)
		if seeders, ok := seederCache[swarmKey]; ok {
			return seeders
		}
		seeders := h.getSeederCount(ctx, torrent)
		seederCache[swarmKey] = seeders
		return seeders
	}

	targetSeeders := getSeederCountCached(targetTorrent)
	targetView := fileSourceTarget{
		RepoID:   targetTorrent.RepoID,
		Revision: targetTorrent.Revision,
		RepoType: targetTorrent.RepoType,
		Path:     targetFile.Path,
		FileRoot: targetFile.FileRoot,
		Size:     targetFile.Size,
		Seeders:  targetSeeders,
	}

	candidateMap := make(map[string]fileSourceCandidate)
	addCandidate := func(torrent models.Torrent, file models.TorrentFile) {
		key := torrent.RepoType + "\x00" + torrent.RepoID + "\x00" + torrent.Revision + "\x00" + file.Path
		seeders := getSeederCountCached(torrent)
		candidateMap[key] = fileSourceCandidate{
			RepoID:      torrent.RepoID,
			Revision:    torrent.Revision,
			RepoType:    torrent.RepoType,
			Path:        file.Path,
			FileRoot:    file.FileRoot,
			Size:        file.Size,
			Seeders:     seeders,
			Score:       float64(seeders),
			AnnounceKey: torrent.AnnounceKey,
		}
	}

	addCandidate(targetTorrent, targetFile)

	processed := map[string]struct{}{
		targetTorrent.ID.Hex(): {},
	}

	if targetFile.FileRoot != "" {
		// Default source discovery stays within the same repo. This matches the
		// current product goal of reusing older revisions of one Hugging Face repo
		// without widening the candidate set to unrelated repos.
		indexedFilter := buildFileSourceLookupFilter(
			repoType,
			repoID,
			targetFile.FileRoot,
			targetFile.Size,
		)

		cursor, err := collection.Find(ctx, indexedFilter)
		if err != nil {
			log.Printf("Failed to query indexed file sources for %s/%s@%s:%s: %v", repoType, repoID, revision, path, err)
			ErrorRes(w, http.StatusInternalServerError, "failed to query file sources")
			return
		}
		defer cursor.Close(ctx)

		var indexedMatches []models.Torrent
		if err := cursor.All(ctx, &indexedMatches); err != nil {
			log.Printf("Failed to decode indexed file source matches: %v", err)
			ErrorRes(w, http.StatusInternalServerError, "failed to decode file sources")
			return
		}

		for _, torrent := range indexedMatches {
			processed[torrent.ID.Hex()] = struct{}{}
			for _, file := range matchingTorrentFiles(torrent, targetFile.FileRoot, targetFile.Size) {
				addCandidate(torrent, file)
			}
		}

		// Backward-compatibility fallback: older documents may not have file_root
		// persisted yet. We scan revisions of the same repo and opportunistically
		// backfill missing file roots from torrent_data so old swarms become usable.
		fallbackCursor, err := collection.Find(ctx, mergeTorrentFilter(bson.M{
			"repo_type": repoType,
			"repo_id":   repoID,
		}))
		if err != nil {
			log.Printf("Failed to query same-repo fallback file sources: %v", err)
			ErrorRes(w, http.StatusInternalServerError, "failed to query fallback file sources")
			return
		}
		defer fallbackCursor.Close(ctx)

		var fallbackMatches []models.Torrent
		if err := fallbackCursor.All(ctx, &fallbackMatches); err != nil {
			log.Printf("Failed to decode fallback file source matches: %v", err)
			ErrorRes(w, http.StatusInternalServerError, "failed to decode fallback file sources")
			return
		}

		for _, torrent := range fallbackMatches {
			if _, seen := processed[torrent.ID.Hex()]; seen {
				continue
			}

			if changed, err := hydrateTorrentFileRoots(&torrent); err != nil {
				log.Printf("Failed to parse fallback torrent_data for %s@%s: %v", torrent.RepoID, torrent.Revision, err)
				continue
			} else if changed {
				h.persistTorrentFiles(ctx, torrent)
			}

			for _, file := range matchingTorrentFiles(torrent, targetFile.FileRoot, targetFile.Size) {
				addCandidate(torrent, file)
			}
		}
	}

	candidates := make([]fileSourceCandidate, 0, len(candidateMap))
	for _, candidate := range candidateMap {
		candidates = append(candidates, candidate)
	}

	sortFileSourceCandidates(candidates, targetView)

	JSONRes(w, http.StatusOK, map[string]any{
		"data": fileSourcesResponse{
			Target:     targetView,
			Candidates: candidates,
		},
	})
}

func sortFileSourceCandidates(candidates []fileSourceCandidate, target fileSourceTarget) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]

		leftExact := left.RepoType == target.RepoType &&
			left.RepoID == target.RepoID &&
			left.Revision == target.Revision &&
			left.Path == target.Path
		rightExact := right.RepoType == target.RepoType &&
			right.RepoID == target.RepoID &&
			right.Revision == target.Revision &&
			right.Path == target.Path

		if leftExact != rightExact {
			return leftExact
		}
		if left.Seeders != right.Seeders {
			return left.Seeders > right.Seeders
		}
		if left.RepoID != right.RepoID {
			return left.RepoID < right.RepoID
		}
		if left.Revision != right.Revision {
			return left.Revision < right.Revision
		}
		return left.Path < right.Path
	})
}
