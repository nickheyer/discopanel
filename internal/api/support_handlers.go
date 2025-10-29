package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SupportBundleResponse represents the response from generating a support bundle
type SupportBundleResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	BundlePath  string `json:"bundle_path,omitempty"`
	ReferenceID string `json:"reference_id,omitempty"`
}

// handleGenerateSupportBundle creates a support bundle with logs and database
func (s *Server) handleGenerateSupportBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.log.Info("Generating support bundle")

	// Create temporary directory for bundle
	tempDir := filepath.Join(s.config.Storage.TempDir, fmt.Sprintf("support-bundle-%d", time.Now().Unix()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		s.log.Error("Failed to create temp directory: %v", err)
		s.respondJSON(w, http.StatusInternalServerError, SupportBundleResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create temp directory: %v", err),
		})
		return
	}
	defer os.RemoveAll(tempDir) // Clean up temp directory when done

	// Prepare bundle file path
	bundleFileName := fmt.Sprintf("discopanel-support-%s.tar.gz", time.Now().Format("20060102-150405"))
	bundlePath := filepath.Join(s.config.Storage.TempDir, bundleFileName)

	// Create the tar.gz file
	bundleFile, err := os.Create(bundlePath)
	if err != nil {
		s.log.Error("Failed to create bundle file: %v", err)
		s.respondJSON(w, http.StatusInternalServerError, SupportBundleResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create bundle file: %v", err),
		})
		return
	}
	defer bundleFile.Close()

	// Create gzip writer
	gzipWriter := gzip.NewWriter(bundleFile)
	defer gzipWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// 1. Add logs to bundle
	if err := s.addLogsToBundle(tarWriter); err != nil {
		s.log.Error("Failed to add logs to bundle: %v", err)
		s.respondJSON(w, http.StatusInternalServerError, SupportBundleResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to add logs: %v", err),
		})
		return
	}

	// 2. Add database copy to bundle
	if err := s.addDatabaseToBundle(tarWriter); err != nil {
		s.log.Error("Failed to add database to bundle: %v", err)
		s.respondJSON(w, http.StatusInternalServerError, SupportBundleResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to add database: %v", err),
		})
		return
	}

	// 3. Add system information
	if err := s.addSystemInfoToBundle(tarWriter); err != nil {
		s.log.Error("Failed to add system info to bundle: %v", err)
		// Don't fail the entire bundle if system info fails
		s.log.Warn("Continuing without system info")
	}

	// Close writers to flush all data
	tarWriter.Close()
	gzipWriter.Close()
	bundleFile.Close()

	// Check if we should upload to support server
	uploadToSupport := r.URL.Query().Get("upload") == "true"

	if uploadToSupport {
		// Upload to support server
		referenceID, err := s.uploadSupportBundle(bundlePath, bundleFileName)
		if err != nil {
			s.log.Error("Failed to upload support bundle: %v", err)
			s.respondJSON(w, http.StatusInternalServerError, SupportBundleResponse{
				Success:    false,
				Message:    fmt.Sprintf("Bundle created but upload failed: %v", err),
				BundlePath: bundlePath,
			})
			return
		}

		// Clean up local bundle after successful upload
		os.Remove(bundlePath)
		s.respondJSON(w, http.StatusOK, SupportBundleResponse{
			Success:     true,
			Message:     "Support bundle uploaded successfully",
			ReferenceID: referenceID,
		})
	} else {
		// Return bundle for download
		s.respondJSON(w, http.StatusOK, SupportBundleResponse{
			Success:    true,
			Message:    "Support bundle created successfully",
			BundlePath: bundlePath,
		})
	}
}

// Add logs to the tar archive
func (s *Server) addLogsToBundle(tarWriter *tar.Writer) error {
	// Get recent logs from memory buffer
	recentLogs := s.log.GetRecentLogs()
	recentLogsContent := strings.Join(recentLogs, "\n")
	header := &tar.Header{
		Name:    "logs/recent-logs.txt",
		Size:    int64(len(recentLogsContent)),
		Mode:    0644,
		ModTime: time.Now(),
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write recent logs header: %w", err)
	}

	if _, err := tarWriter.Write([]byte(recentLogsContent)); err != nil {
		return fmt.Errorf("failed to write recent logs content: %w", err)
	}

	// Add log file if it exists
	logFilePath := s.log.GetLogFilePath()
	if logFilePath != "" && fileExists(logFilePath) {
		file, err := os.Open(logFilePath)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat log file: %w", err)
		}

		header := &tar.Header{
			Name:    "logs/discopanel.log",
			Size:    stat.Size(),
			Mode:    0644,
			ModTime: stat.ModTime(),
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write log file header: %w", err)
		}

		if _, err := io.Copy(tarWriter, file); err != nil {
			return fmt.Errorf("failed to write log file content: %w", err)
		}
	}

	// Add any rotated log files
	logDir := filepath.Dir(s.log.GetLogFilePath())
	if logDir != "" && logDir != "." {
		files, _ := os.ReadDir(logDir)
		for _, file := range files {
			if strings.HasPrefix(file.Name(), "discopanel") && strings.Contains(file.Name(), ".log") {
				fullPath := filepath.Join(logDir, file.Name())
				if err := addFileToTar(tarWriter, fullPath, filepath.Join("logs", file.Name())); err != nil {
					s.log.Warn("Failed to add log file %s: %v", file.Name(), err)
				}
			}
		}
	}

	return nil
}

// Add db to the tar archive
func (s *Server) addDatabaseToBundle(tarWriter *tar.Writer) error {
	dbPath := s.config.Database.Path

	if !fileExists(dbPath) {
		return fmt.Errorf("database file not found at %s", dbPath)
	}

	// Copy database file to tar
	return addFileToTar(tarWriter, dbPath, "database/discopanel.db")
}

// Add system and configuration information to bundle
func (s *Server) addSystemInfoToBundle(tarWriter *tar.Writer) error {
	servers, _ := s.store.ListServers(context.Background())
	info := map[string]any{
		"timestamp":    time.Now().Format(time.RFC3339),
		"version":      getVersionInfo(),
		"server_count": len(servers),
		"config":       s.config,
	}

	jsonData, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal system info: %w", err)
	}

	header := &tar.Header{
		Name:    "system-info.json",
		Size:    int64(len(jsonData)),
		Mode:    0644,
		ModTime: time.Now(),
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write system info header: %w", err)
	}

	if _, err := tarWriter.Write(jsonData); err != nil {
		return fmt.Errorf("failed to write system info content: %w", err)
	}

	return nil
}

// Upload bundle to support server
func (s *Server) uploadSupportBundle(bundlePath, fileName string) (string, error) {
	supportURL := getUploadSupportUrl()

	// Open the bundle file
	file, err := os.Open(bundlePath)
	if err != nil {
		return "", fmt.Errorf("failed to open bundle file: %w", err)
	}
	defer file.Close()

	// Get file info
	fileInfo, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat bundle file: %w", err)
	}

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file field
	part, err := writer.CreateFormFile("bundle", fileName)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}

	// Copy file content
	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("failed to copy file content: %w", err)
	}

	// Add metadata fields
	writer.WriteField("timestamp", time.Now().Format(time.RFC3339))
	writer.WriteField("size", fmt.Sprintf("%d", fileInfo.Size()))

	// Close multipart writer
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create request
	req, err := http.NewRequest(http.MethodPost, supportURL, body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to upload bundle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload failed with status: %d", resp.StatusCode)
	}

	var uploadResp struct {
		URL         string `json:"url"`
		Message     string `json:"message"`
		ReferenceID string `json:"reference_id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return supportURL, nil
	}

	// Return the reference ID as part of the URL if provided
	if uploadResp.ReferenceID != "" {
		return uploadResp.ReferenceID, nil
	}

	return uploadResp.URL, nil
}

// handleDownloadSupportBundle allows downloading a previously generated support bundle
func (s *Server) handleDownloadSupportBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bundlePath := r.URL.Query().Get("path")
	if bundlePath == "" {
		http.Error(w, "Bundle path required", http.StatusBadRequest)
		return
	}

	// Security: ensure the path is within our temp directory
	if !strings.HasPrefix(bundlePath, s.config.Storage.TempDir) {
		http.Error(w, "Invalid bundle path", http.StatusBadRequest)
		return
	}

	if !fileExists(bundlePath) {
		http.Error(w, "Bundle not found", http.StatusNotFound)
		return
	}

	// Set headers for download
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(bundlePath)))

	// Stream the file
	file, err := os.Open(bundlePath)
	if err != nil {
		http.Error(w, "Failed to open bundle", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	io.Copy(w, file)

	// Clean up the bundle file after download
	go func() {
		time.Sleep(5 * time.Second)
		os.Remove(bundlePath)
	}()
}

// Add file to archive
func addFileToTar(tw *tar.Writer, sourcePath, destPath string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    destPath,
		Size:    stat.Size(),
		Mode:    0644,
		ModTime: stat.ModTime(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, file)
	return err
}

// Helper function to check if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func getSupportUrl() string {
	url := os.Getenv("SUPPORT_BASE_URL")
	if url == "" {
		url = "https://support.discopanel.app"
	}
	return url
}

func getUploadSupportUrl() string {
	return getSupportUrl() + "/api/v1/uploads"
}

func getVersionInfo() string {
	appV := os.Getenv("APP_VERSION")
	if appV != "" {
		return appV
	}

	info, err := buildinfo.ReadFile(os.Args[0])
	if err != nil {
		fmt.Println("Could not read build info")
		return ""
	}

	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}

	return ""
}
