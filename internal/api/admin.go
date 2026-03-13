package api

import (
	"log"
	"net/http"
	"os"
	"strings"

	"llmpt/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// adminAuthMiddleware verifies the Bearer token matches ADMIN_TOKEN
func adminAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		adminToken := os.Getenv("ADMIN_TOKEN")
		if adminToken == "" {
			// If not configured, deny access by default for safety
			ErrorRes(w, http.StatusForbidden, "Admin features are not configured")
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			ErrorRes(w, http.StatusUnauthorized, "missing or invalid Authorization header")
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != adminToken {
			ErrorRes(w, http.StatusUnauthorized, "invalid admin token")
			return
		}

		next.ServeHTTP(w, r)
	}
}

// AdminListTorrents 获取所有种子 (GET /api/v1/admin/torrents)
func (h *Handler) AdminListTorrents(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		ErrorRes(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	collection := h.db.MongoDB.TorrentsCollection()

	findOptions := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := collection.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		log.Printf("Failed to fetch torrents for admin: %v", err)
		ErrorRes(w, http.StatusInternalServerError, "Failed to fetch torrents")
		return
	}
	defer cursor.Close(ctx)

	var torrents []models.Torrent
	if err = cursor.All(ctx, &torrents); err != nil {
		log.Printf("Failed to decode admin torrents: %v", err)
		ErrorRes(w, http.StatusInternalServerError, "Failed to decode torrents")
		return
	}

	if torrents == nil {
		torrents = []models.Torrent{}
	}

	JSONRes(w, http.StatusOK, map[string]interface{}{
		"total": len(torrents),
		"data":  torrents,
	})
}

// AdminApproveTorrent 审核通过种子 (POST /api/v1/admin/torrents/:id/approve)
func (h *Handler) AdminApproveTorrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		ErrorRes(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Simple manual path parsing since we are using ServeMux
	// URL: /api/v1/admin/torrents/{id}/approve
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 {
		ErrorRes(w, http.StatusBadRequest, "invalid url path")
		return
	}
	idStr := parts[5]

	objID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		ErrorRes(w, http.StatusBadRequest, "invalid torrent id")
		return
	}

	ctx := r.Context()
	collection := h.db.MongoDB.TorrentsCollection()

	filter := bson.M{"_id": objID}
	update := bson.M{"$set": bson.M{"status": "active"}}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		log.Printf("Failed to approve torrent %s: %v", idStr, err)
		ErrorRes(w, http.StatusInternalServerError, "failed to update torrent status")
		return
	}

	if result.ModifiedCount == 0 {
		JSONRes(w, http.StatusOK, map[string]string{"message": "torrent was already active or not found"})
		return
	}

	JSONRes(w, http.StatusOK, map[string]string{"message": "torrent approved successfully"})
}

// AdminDeleteTorrent 强行删除种子 (DELETE /api/v1/admin/torrents/:id)
func (h *Handler) AdminDeleteTorrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		ErrorRes(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 {
		ErrorRes(w, http.StatusBadRequest, "invalid url path")
		return
	}
	idStr := parts[5]

	objID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		ErrorRes(w, http.StatusBadRequest, "invalid torrent id")
		return
	}

	ctx := r.Context()
	collection := h.db.MongoDB.TorrentsCollection()

	// First find the torrent to get info_hash
	var torrent models.Torrent
	err = collection.FindOne(ctx, bson.M{"_id": objID}).Decode(&torrent)
	if err != nil {
		ErrorRes(w, http.StatusNotFound, "torrent not found")
		return
	}

	// Delete from MongoDB
	_, err = collection.DeleteOne(ctx, bson.M{"_id": objID})
	if err != nil {
		log.Printf("Failed to delete torrent DB record %s: %v", idStr, err)
		ErrorRes(w, http.StatusInternalServerError, "failed to delete torrent")
		return
	}

	// Optinal: Delete peer dicts from Redis tracker
	// (This cleans up the Tracker's memory for this swarm entirely)
	swarmKey := torrent.AnnounceKey
	if swarmKey == "" {
		swarmKey = torrent.InfoHash
	}
	deleteKeys := []string{
		"tracker:seeders:" + swarmKey,
		"tracker:leechers:" + swarmKey,
		"tracker:stats:" + swarmKey,
	}
	h.db.Redis.Client.Del(ctx, deleteKeys...)
	h.db.Redis.Client.SRem(ctx, "tracker:active_torrents", swarmKey)

	JSONRes(w, http.StatusOK, map[string]string{"message": "torrent and tracker info deleted successfully"})
}
