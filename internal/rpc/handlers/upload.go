package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nickheyer/discopanel/internal/auth"
	"github.com/nickheyer/discopanel/internal/rbac"
	"github.com/nickheyer/discopanel/pkg/logger"
	"github.com/nickheyer/discopanel/pkg/upload"
)

type uploadStreamResponse struct {
	SessionID     string `json:"session_id"`
	BytesReceived int64  `json:"bytes_received"`
	Completed     bool   `json:"completed"`
	TempPath      string `json:"temp_path,omitempty"`
}

// NewUploadStreamHandler creates an HTTP handler for streaming file uploads.
//
//	PUT /api/v1/upload/{sessionId}
//	Headers:
//	  Authorization: Bearer <token>
//	  Content-Type: application/octet-stream
//	  X-Upload-Offset: <byte offset for resume> (optional, default 0)
//	Body: raw file bytes (or file slice for resume)
func NewUploadStreamHandler(uploadManager *upload.Manager, authManager *auth.Manager, enforcer *rbac.Enforcer, log *logger.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract session ID from path
		sessionID := strings.TrimPrefix(r.URL.Path, "/api/v1/upload/")
		if sessionID == "" || strings.Contains(sessionID, "/") {
			http.Error(w, "invalid session_id", http.StatusBadRequest)
			return
		}

		// Authenticate using the shared auth logic
		user, err := authManager.AuthenticateFromHeader(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Check RBAC permission (uploads:create)
		if enforcer != nil {
			allowed, rbacErr := enforcer.Enforce(user.Roles, rbac.ResourceUploads, rbac.ActionCreate, "*")
			if rbacErr != nil || !allowed {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}

		// Parse resume offset
		offset := int64(0)
		if offsetStr := r.Header.Get("X-Upload-Offset"); offsetStr != "" {
			offset, err = strconv.ParseInt(offsetStr, 10, 64)
			if err != nil || offset < 0 {
				http.Error(w, "invalid X-Upload-Offset", http.StatusBadRequest)
				return
			}
		}

		// Extend server read/write timeout
		rc := http.NewResponseController(w)
		if err := rc.SetReadDeadline(time.Now().Add(30 * time.Minute)); err != nil {
			log.Warn("Failed to set read deadline: %v", err)
		}
		if err := rc.SetWriteDeadline(time.Now().Add(30 * time.Minute)); err != nil {
			log.Warn("Failed to set write deadline: %v", err)
		}

		// Stream request body into the upload session file
		bytesWritten, completed, err := uploadManager.WriteStream(sessionID, r.Body, offset)
		if err != nil {
			log.Error("Stream upload error for session %s: %v", sessionID, err)
			resp := uploadStreamResponse{
				SessionID:     sessionID,
				BytesReceived: offset + bytesWritten,
				Completed:     false,
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(resp)
			return
		}

		resp := uploadStreamResponse{
			SessionID:     sessionID,
			BytesReceived: offset + bytesWritten,
			Completed:     completed,
		}

		if completed {
			if tempPath, _, tempErr := uploadManager.GetTempPath(sessionID); tempErr == nil {
				resp.TempPath = tempPath
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
}
