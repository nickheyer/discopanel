package services

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that SupportService implements the interface
var _ discopanelv1connect.SupportServiceHandler = (*SupportService)(nil)

// SupportService implements the Support service
type SupportService struct {
	store  *storage.Store
	docker *docker.Client
	config *config.Config
	log    *logger.Logger
}

// NewSupportService creates a new support service
func NewSupportService(store *storage.Store, docker *docker.Client, cfg *config.Config, log *logger.Logger) *SupportService {
	return &SupportService{
		store:  store,
		docker: docker,
		config: cfg,
		log:    log,
	}
}

// GenerateSupportBundle generates a support bundle
func (s *SupportService) GenerateSupportBundle(ctx context.Context, req *connect.Request[v1.GenerateSupportBundleRequest]) (*connect.Response[v1.GenerateSupportBundleResponse], error) {
	s.log.Info("Generating support bundle")

	// Create temporary directory for bundle
	tempDir := filepath.Join(s.config.Storage.TempDir, fmt.Sprintf("support-bundle-%d", time.Now().Unix()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		s.log.Error("Failed to create temp directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create temp directory: %w", err))
	}
	defer os.RemoveAll(tempDir) // Clean up temp directory when done

	// Prepare bundle file path
	bundleFileName := fmt.Sprintf("discopanel-support-%s.tar.gz", time.Now().Format("20060102-150405"))
	bundlePath := filepath.Join(s.config.Storage.TempDir, bundleFileName)

	// Create the tar.gz file
	bundleFile, err := os.Create(bundlePath)
	if err != nil {
		s.log.Error("Failed to create bundle file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create bundle file: %w", err))
	}
	defer bundleFile.Close()

	// Create gzip writer
	gzipWriter := gzip.NewWriter(bundleFile)
	defer gzipWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Add logs to bundle if requested
	if req.Msg.IncludeLogs {
		if err := s.addLogsToBundle(tarWriter); err != nil {
			s.log.Error("Failed to add logs to bundle: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to add logs: %w", err))
		}
	}

	// Add database copy to bundle if requested
	if req.Msg.IncludeConfigs {
		if err := s.addDatabaseToBundle(tarWriter); err != nil {
			s.log.Error("Failed to add database to bundle: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to add database: %w", err))
		}
	}

	// Add system information if requested
	if req.Msg.IncludeSystemInfo {
		if err := s.addSystemInfoToBundle(ctx, tarWriter); err != nil {
			s.log.Error("Failed to add system info to bundle: %v", err)
			// Don't fail the entire bundle if system info fails
			s.log.Warn("Continuing without system info")
		}
	}

	// Close writers to flush all data
	tarWriter.Close()
	gzipWriter.Close()
	bundleFile.Close()

	// Get file stats
	fileInfo, err := os.Stat(bundlePath)
	if err != nil {
		s.log.Error("Failed to stat bundle file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stat bundle file: %w", err))
	}

	// Generate bundle ID (use filename without extension)
	bundleID := strings.TrimSuffix(bundleFileName, ".tar.gz")

	return connect.NewResponse(&v1.GenerateSupportBundleResponse{
		BundleId:  bundleID,
		Filename:  bundleFileName,
		Size:      fileInfo.Size(),
		CreatedAt: timestamppb.New(fileInfo.ModTime()),
		Message:   "Support bundle created successfully",
	}), nil
}

// DownloadSupportBundle downloads a support bundle
func (s *SupportService) DownloadSupportBundle(ctx context.Context, req *connect.Request[v1.DownloadSupportBundleRequest]) (*connect.Response[v1.DownloadSupportBundleResponse], error) {
	bundleID := req.Msg.BundleId
	if bundleID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("bundle_id is required"))
	}

	// Construct the bundle file path
	bundleFileName := bundleID + ".tar.gz"
	bundlePath := filepath.Join(s.config.Storage.TempDir, bundleFileName)

	// Security: ensure the path is within our temp directory
	if !strings.HasPrefix(bundlePath, s.config.Storage.TempDir) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid bundle path"))
	}

	// Check if file exists
	if !fileExists(bundlePath) {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("bundle not found"))
	}

	// Read the bundle file
	content, err := os.ReadFile(bundlePath)
	if err != nil {
		s.log.Error("Failed to read bundle file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read bundle file: %w", err))
	}

	// Clean up the bundle file after download (async)
	go func() {
		time.Sleep(5 * time.Second)
		os.Remove(bundlePath)
	}()

	return connect.NewResponse(&v1.DownloadSupportBundleResponse{
		Content:  content,
		Filename: bundleFileName,
		MimeType: "application/gzip",
	}), nil
}

// Add logs to the tar archive
func (s *SupportService) addLogsToBundle(tarWriter *tar.Writer) error {
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
func (s *SupportService) addDatabaseToBundle(tarWriter *tar.Writer) error {
	dbPath := s.config.Database.Path

	if !fileExists(dbPath) {
		return fmt.Errorf("database file not found at %s", dbPath)
	}

	// Copy database file to tar
	return addFileToTar(tarWriter, dbPath, "database/discopanel.db")
}

// Add system and configuration information to bundle
func (s *SupportService) addSystemInfoToBundle(ctx context.Context, tarWriter *tar.Writer) error {
	servers, _ := s.store.ListServers(ctx)
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

func getVersionInfo() string {
	appV := os.Getenv("APP_VERSION")
	if appV != "" {
		return appV
	}
	return "unknown"
}
