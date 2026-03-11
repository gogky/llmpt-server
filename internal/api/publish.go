package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"

	"llmpt/internal/models"
	"llmpt/internal/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// commitHashRe matches a 40-character lowercase hexadecimal string (Git commit hash).
var commitHashRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// PublishRequest 发布模型的请求结构
type PublishRequest struct {
	RepoID      string               `json:"repo_id"`
	Revision    string               `json:"revision"`
	RepoType    string               `json:"repo_type"`
	Name        string               `json:"name"`
	InfoHash    string               `json:"info_hash"`
	TotalSize   int64                `json:"total_size"`
	FileCount   int                  `json:"file_count"`
	TorrentData string               `json:"torrent_data"` // base64-encoded .torrent file
	PieceLength int64                `json:"piece_length"`
	Files       []models.TorrentFile `json:"files"`
}

// PublishTorrent 接收并发布新的模型元数据 (POST /api/v1/publish)
func (h *Handler) PublishTorrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		ErrorRes(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req PublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ErrorRes(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 基础校验
	if req.InfoHash == "" || req.RepoID == "" || req.Revision == "" {
		ErrorRes(w, http.StatusBadRequest, "info_hash, repo_id, and revision are required")
		return
	}

	if req.TorrentData == "" {
		ErrorRes(w, http.StatusBadRequest, "torrent_data is required (base64-encoded .torrent file)")
		return
	}

	// Revision 必须是 40 字符的 commit hash，拒绝分支名等非规范化值
	if !commitHashRe.MatchString(req.Revision) {
		ErrorRes(w, http.StatusBadRequest,
			"revision must be a 40-character commit hash (e.g. 'abc123...'), not a branch name like 'main'. "+
				"Please resolve the revision to a commit hash on the client side before publishing.")
		return
	}

	// Decode base64 torrent data
	torrentBytes, err := base64.StdEncoding.DecodeString(req.TorrentData)
	if err != nil {
		ErrorRes(w, http.StatusBadRequest, "torrent_data must be valid base64")
		return
	}

	// 1. 服务端解析 Bencode 并覆写元数据
	meta, err := utils.ParseTorrent(torrentBytes)
	if err != nil {
		ErrorRes(w, http.StatusBadRequest, "invalid torrent bencode: "+err.Error())
		return
	}

	if meta.InfoHash != req.InfoHash {
		ErrorRes(w, http.StatusBadRequest, "info_hash mismatch: client provided "+req.InfoHash+" but we calculated "+meta.InfoHash)
		return
	}

	// 校验切片大小是否符合规范
	expectedPieceLength := utils.GetOptimalPieceLength(meta.TotalSize)
	if meta.PieceLength != expectedPieceLength {
		ErrorRes(w, http.StatusBadRequest, "piece_length mismatch for this size")
		return
	}

	// 校验种子根目录名是否为 commit hash
	if meta.Name != req.Revision {
		ErrorRes(w, http.StatusBadRequest, "torrent name (root directory) must match the revision commit hash exactly")
		return
	}

	// 2. 检查唯一的 InfoHash，同一 revision 不允许出现不同 content
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	collection := h.db.MongoDB.TorrentsCollection()
	if req.RepoType == "" {
		req.RepoType = "model"
	}

	var existing models.Torrent
	err = collection.FindOne(ctx, bson.M{"repo_type": req.RepoType, "repo_id": req.RepoID, "revision": req.Revision}).Decode(&existing)
	if err == nil {
		// Found existing
		if existing.InfoHash != meta.InfoHash {
			ErrorRes(w, http.StatusConflict, "A different torrent info_hash already exists for this revision")
			return
		}
		// Idempotent success
		JSONRes(w, http.StatusOK, map[string]string{
			"message":   "torrent metadata already exists",
			"info_hash": req.InfoHash,
		})
		return
	} else if err != mongo.ErrNoDocuments {
		ErrorRes(w, http.StatusInternalServerError, "database error")
		return
	}

	// 3. Hugging Face 远端树清单比对
	hfToken := os.Getenv("HF_TOKEN")
	hfMap, status, hfErr := utils.FetchHFManifest(req.RepoType, req.RepoID, req.Revision, hfToken)

	finalStatus := "active"
	if hfErr != nil || status == http.StatusUnauthorized || status == http.StatusForbidden {
		// Log error, set status to pending. Let admins approve it.
		log.Printf("HF api failed or access denied for %s/%s@%s. Status: %d. Error: %v", req.RepoType, req.RepoID, req.Revision, status, hfErr)
		finalStatus = "pending"
	} else {
		// 精确比对 files
		if len(meta.Files) != len(hfMap) {
			ErrorRes(w, http.StatusBadRequest, "file count mismatch with HF manifest")
			return
		}
		for _, f := range meta.Files {
			hfSize, ok := hfMap[f.Path]
			if !ok {
				ErrorRes(w, http.StatusBadRequest, "file not found in HF manifest: "+f.Path)
				return
			}
			if f.Size != hfSize {
				ErrorRes(w, http.StatusBadRequest, "file size mismatch for "+f.Path)
				return
			}
		}
	}

	// 转换为数据库存储格式
	dbFiles := make([]models.TorrentFile, 0, len(meta.Files))
	for _, f := range meta.Files {
		dbFiles = append(dbFiles, models.TorrentFile{
			Path: f.Path,
			Size: f.Size,
		})
	}

	newTorrent := models.Torrent{
		ID:          primitive.NewObjectID(),
		RepoID:      req.RepoID,
		Revision:    req.Revision,
		RepoType:    req.RepoType,
		Name:        req.Name,
		InfoHash:    meta.InfoHash,
		TotalSize:   meta.TotalSize,
		FileCount:   meta.FileCount,
		TorrentData: torrentBytes,
		PieceLength: meta.PieceLength,
		Files:       dbFiles,
		Status:      finalStatus,
		CreatedAt:   time.Now(),
	}

	_, err = collection.InsertOne(ctx, newTorrent)
	if err != nil {
		log.Printf("Failed to insert torrent: %v", err)
		ErrorRes(w, http.StatusInternalServerError, "failed to save torrent metadata")
		return
	}

	msg := "torrent metadata created"
	if finalStatus == "pending" {
		msg = "torrent requires admin approval (pending)"
	}

	JSONRes(w, http.StatusOK, map[string]string{
		"message":   msg,
		"info_hash": req.InfoHash,
		"status":    finalStatus,
	})
}
