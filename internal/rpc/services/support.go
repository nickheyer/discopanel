package services

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

	"connectrpc.com/connect"
	"github.com/google/uuid"
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
	// Store generated bundles temporarily
	bundles map[string]*BundleInfo
}

// BundleInfo stores information about a generated bundle
type BundleInfo struct {
	ID        string
	Filename  string
	Path      string
	Size      int64
	CreatedAt time.Time
}

// UploadUserInfo contains user-provided contact and issue information
type UploadUserInfo struct {
	DiscordUsername  string
	Email            string
	GithubUsername   string
	IssueDescription string
	StepsToReproduce string
}

// NewSupportService creates a new support service
func NewSupportService(store *storage.Store, docker *docker.Client, config *config.Config, log *logger.Logger) *SupportService {
	return &SupportService{
		store:   store,
		docker:  docker,
		config:  config,
		log:     log,
		bundles: make(map[string]*BundleInfo),
	}
}

// GenerateSupportBundle generates a support bundle
func (s *SupportService) GenerateSupportBundle(ctx context.Context, req *connect.Request[v1.GenerateSupportBundleRequest]) (*connect.Response[v1.GenerateSupportBundleResponse], error) {
	msg := req.Msg

	// Default all options to true if not specified
	includeLogs := msg.IncludeLogs
	includeConfigs := msg.IncludeConfigs
	includeSystemInfo := msg.IncludeSystemInfo

	s.log.Info("Generating support bundle (logs=%v, configs=%v, system=%v)", includeLogs, includeConfigs, includeSystemInfo)

	// Create temporary directory for bundle
	tempDir := filepath.Join(s.config.Storage.TempDir, fmt.Sprintf("support-bundle-%d", time.Now().Unix()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		s.log.Error("Failed to create temp directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create temp directory"))
	}
	defer os.RemoveAll(tempDir) // Clean up temp directory when done

	// Prepare bundle file path
	bundleID := uuid.New().String()
	bundleFileName := fmt.Sprintf("discopanel-support-%s.tar.gz", time.Now().Format("20060102-150405"))
	bundlePath := filepath.Join(s.config.Storage.TempDir, bundleFileName)

	// Create the tar.gz file
	bundleFile, err := os.Create(bundlePath)
	if err != nil {
		s.log.Error("Failed to create bundle file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create bundle file"))
	}
	defer bundleFile.Close()

	// Create gzip writer
	gzipWriter := gzip.NewWriter(bundleFile)
	defer gzipWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// 1. Add logs to bundle if requested
	if includeLogs {
		if err := s.addLogsToBundle(ctx, tarWriter, msg.ServerIds); err != nil {
			s.log.Error("Failed to add logs to bundle: %v", err)
			// Continue without failing the entire bundle
			s.log.Warn("Continuing without logs")
		}
	}

	// 2. Add database/configs to bundle if requested
	if includeConfigs {
		if err := s.addDatabaseToBundle(tarWriter); err != nil {
			s.log.Error("Failed to add database to bundle: %v", err)
			// Continue without failing
			s.log.Warn("Continuing without database")
		}

		// Add server configurations
		if err := s.addServerConfigsToBundle(ctx, tarWriter, msg.ServerIds); err != nil {
			s.log.Error("Failed to add server configs: %v", err)
			s.log.Warn("Continuing without server configs")
		}
	}

	// 3. Add system information if requested
	if includeSystemInfo {
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

	// Get file size
	fileInfo, err := os.Stat(bundlePath)
	if err != nil {
		s.log.Error("Failed to stat bundle file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get bundle info"))
	}

	// Store bundle info for download
	bundleInfo := &BundleInfo{
		ID:        bundleID,
		Filename:  bundleFileName,
		Path:      bundlePath,
		Size:      fileInfo.Size(),
		CreatedAt: time.Now(),
	}
	s.bundles[bundleID] = bundleInfo

	// Clean up old bundles after 1 hour
	go func() {
		time.Sleep(1 * time.Hour)
		s.cleanupBundle(bundleID)
	}()

	return connect.NewResponse(&v1.GenerateSupportBundleResponse{
		BundleId:  bundleID,
		Filename:  bundleFileName,
		Size:      fileInfo.Size(),
		CreatedAt: timestamppb.New(bundleInfo.CreatedAt),
		Message:   "Support bundle created successfully",
	}), nil
}

// DownloadSupportBundle downloads a support bundle
func (s *SupportService) DownloadSupportBundle(ctx context.Context, req *connect.Request[v1.DownloadSupportBundleRequest]) (*connect.Response[v1.DownloadSupportBundleResponse], error) {
	bundleInfo, exists := s.bundles[req.Msg.BundleId]
	if !exists {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("bundle not found or expired"))
	}

	// Read the bundle file
	bundleData, err := os.ReadFile(bundleInfo.Path)
	if err != nil {
		s.log.Error("Failed to read bundle file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read bundle"))
	}

	// Clean up the bundle after download
	go s.cleanupBundle(req.Msg.BundleId)

	return connect.NewResponse(&v1.DownloadSupportBundleResponse{
		Content:  bundleData,
		Filename: bundleInfo.Filename,
		MimeType: "application/gzip",
	}), nil
}

// UploadSupportBundle generates and uploads a support bundle to the support server
func (s *SupportService) UploadSupportBundle(ctx context.Context, req *connect.Request[v1.UploadSupportBundleRequest]) (*connect.Response[v1.UploadSupportBundleResponse], error) {
	msg := req.Msg

	// Default all options to true if not specified
	includeLogs := msg.IncludeLogs
	includeConfigs := msg.IncludeConfigs
	includeSystemInfo := msg.IncludeSystemInfo

	s.log.Info("Generating support bundle for upload (logs=%v, configs=%v, system=%v)", includeLogs, includeConfigs, includeSystemInfo)

	// Create temporary directory for bundle
	tempDir := filepath.Join(s.config.Storage.TempDir, fmt.Sprintf("support-bundle-%d", time.Now().Unix()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		s.log.Error("Failed to create temp directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create temp directory"))
	}
	defer os.RemoveAll(tempDir) // Clean up temp directory when done

	// Prepare bundle file path
	bundleFileName := fmt.Sprintf("discopanel-support-%s.tar.gz", time.Now().Format("20060102-150405"))
	bundlePath := filepath.Join(s.config.Storage.TempDir, bundleFileName)

	// Create the tar.gz file
	bundleFile, err := os.Create(bundlePath)
	if err != nil {
		s.log.Error("Failed to create bundle file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create bundle file"))
	}
	defer bundleFile.Close()

	// Create gzip writer
	gzipWriter := gzip.NewWriter(bundleFile)
	defer gzipWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// 1. Add logs to bundle if requested
	if includeLogs {
		if err := s.addLogsToBundle(ctx, tarWriter, msg.ServerIds); err != nil {
			s.log.Error("Failed to add logs to bundle: %v", err)
			// Continue without failing the entire bundle
			s.log.Warn("Continuing without logs")
		}
	}

	// 2. Add database/configs to bundle if requested
	if includeConfigs {
		if err := s.addDatabaseToBundle(tarWriter); err != nil {
			s.log.Error("Failed to add database to bundle: %v", err)
			// Continue without failing
			s.log.Warn("Continuing without database")
		}

		// Add server configurations
		if err := s.addServerConfigsToBundle(ctx, tarWriter, msg.ServerIds); err != nil {
			s.log.Error("Failed to add server configs: %v", err)
			s.log.Warn("Continuing without server configs")
		}
	}

	// 3. Add system information if requested
	if includeSystemInfo {
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

	// Build user info for upload
	userInfo := &UploadUserInfo{
		DiscordUsername:  msg.DiscordUsername,
		Email:            msg.Email,
		GithubUsername:   msg.GithubUsername,
		IssueDescription: msg.IssueDescription,
		StepsToReproduce: msg.StepsToReproduce,
	}

	// Upload the bundle to support server
	referenceID, err := s.uploadBundleToServer(bundlePath, bundleFileName, userInfo)
	if err != nil {
		s.log.Error("Failed to upload support bundle: %v", err)
		// Clean up the bundle file
		os.Remove(bundlePath)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to upload support bundle: %v", err))
	}

	// Clean up the bundle file after successful upload
	os.Remove(bundlePath)

	return connect.NewResponse(&v1.UploadSupportBundleResponse{
		ReferenceId: referenceID,
		Message:     "Support bundle uploaded successfully",
		Success:     true,
	}), nil
}

// uploadBundleToServer uploads a bundle file to the support server
func (s *SupportService) uploadBundleToServer(bundlePath, fileName string, userInfo *UploadUserInfo) (string, error) {
	supportURL := s.getUploadSupportUrl()

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

	// Add user-provided contact and issue information
	if userInfo != nil {
		if userInfo.DiscordUsername != "" {
			writer.WriteField("discord_username", userInfo.DiscordUsername)
		}
		if userInfo.Email != "" {
			writer.WriteField("email", userInfo.Email)
		}
		if userInfo.GithubUsername != "" {
			writer.WriteField("github_username", userInfo.GithubUsername)
		}
		if userInfo.IssueDescription != "" {
			writer.WriteField("issue_description", userInfo.IssueDescription)
		}
		if userInfo.StepsToReproduce != "" {
			writer.WriteField("steps_to_reproduce", userInfo.StepsToReproduce)
		}
	}

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
		// If we can't decode the response, just return the support URL
		return supportURL, nil
	}

	// Return the reference ID if provided
	if uploadResp.ReferenceID != "" {
		return uploadResp.ReferenceID, nil
	}

	return uploadResp.URL, nil
}

// Helper method to get the support server URL
func (s *SupportService) getSupportUrl() string {
	url := os.Getenv("SUPPORT_BASE_URL")
	if url == "" {
		url = "https://support.discopanel.app"
	}
	return url
}

// Helper method to get the upload support URL
func (s *SupportService) getUploadSupportUrl() string {
	return s.getSupportUrl() + "/api/v1/uploads"
}

// cleanupBundle removes a bundle from memory and disk
func (s *SupportService) cleanupBundle(bundleID string) {
	if bundleInfo, exists := s.bundles[bundleID]; exists {
		// Remove file
		os.Remove(bundleInfo.Path)
		// Remove from map
		delete(s.bundles, bundleID)
		s.log.Debug("Cleaned up support bundle %s", bundleID)
	}
}

// addLogsToBundle adds logs to the tar archive
func (s *SupportService) addLogsToBundle(ctx context.Context, tarWriter *tar.Writer, serverIDs []string) error {
	// Get recent logs from memory buffer
	recentLogs := s.log.GetRecentLogs()
	recentLogsContent := strings.Join(recentLogs, "\n")

	// Add recent logs
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
		if err := addFileToTar(tarWriter, logFilePath, "logs/discopanel.log"); err != nil {
			return fmt.Errorf("failed to add log file: %w", err)
		}
	}

	// Add any rotated log files
	if logFilePath != "" {
		logDir := filepath.Dir(logFilePath)
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
	}

	// Add server-specific logs if requested
	if len(serverIDs) > 0 {
		for _, serverID := range serverIDs {
			server, err := s.store.GetServer(ctx, serverID)
			if err != nil {
				s.log.Warn("Failed to get server %s for logs: %v", serverID, err)
				continue
			}

			// Add server's latest.log if it exists
			latestLogPath := filepath.Join(server.DataPath, "logs", "latest.log")
			if fileExists(latestLogPath) {
				targetPath := fmt.Sprintf("servers/%s/latest.log", server.Name)
				if err := addFileToTar(tarWriter, latestLogPath, targetPath); err != nil {
					s.log.Warn("Failed to add server log for %s: %v", server.Name, err)
				}
			}
		}
	}

	return nil
}

// addDatabaseToBundle adds the database to the tar archive
func (s *SupportService) addDatabaseToBundle(tarWriter *tar.Writer) error {
	dbPath := s.config.Database.Path

	if !fileExists(dbPath) {
		return fmt.Errorf("database file not found at %s", dbPath)
	}

	// Copy database file to tar
	return addFileToTar(tarWriter, dbPath, "database/discopanel.db")
}

// addServerConfigsToBundle adds server configuration files to the bundle
func (s *SupportService) addServerConfigsToBundle(ctx context.Context, tarWriter *tar.Writer, serverIDs []string) error {
	var servers []*storage.Server
	var err error

	if len(serverIDs) > 0 {
		// Get specific servers
		for _, id := range serverIDs {
			server, err := s.store.GetServer(ctx, id)
			if err == nil {
				servers = append(servers, server)
			}
		}
	} else {
		// Get all servers
		servers, err = s.store.ListServers(ctx)
		if err != nil {
			return fmt.Errorf("failed to list servers: %w", err)
		}
	}

	// Add each server's configuration
	for _, server := range servers {
		// Get server config from database
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Warn("Failed to get config for server %s: %v", server.Name, err)
			continue
		}

		// Marshal server config to JSON
		configData, err := json.MarshalIndent(serverConfig, "", "  ")
		if err != nil {
			s.log.Warn("Failed to marshal config for server %s: %v", server.Name, err)
			continue
		}

		// Add to tar
		header := &tar.Header{
			Name:    fmt.Sprintf("configs/servers/%s_config.json", server.Name),
			Size:    int64(len(configData)),
			Mode:    0644,
			ModTime: time.Now(),
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write config header for %s: %w", server.Name, err)
		}

		if _, err := tarWriter.Write(configData); err != nil {
			return fmt.Errorf("failed to write config content for %s: %w", server.Name, err)
		}

		// Also add server.properties if it exists
		serverPropsPath := filepath.Join(server.DataPath, "server.properties")
		if fileExists(serverPropsPath) {
			targetPath := fmt.Sprintf("configs/servers/%s_server.properties", server.Name)
			if err := addFileToTar(tarWriter, serverPropsPath, targetPath); err != nil {
				s.log.Warn("Failed to add server.properties for %s: %v", server.Name, err)
			}
		}
	}

	return nil
}

// addSystemInfoToBundle adds system and configuration information to bundle
func (s *SupportService) addSystemInfoToBundle(ctx context.Context, tarWriter *tar.Writer) error {
	servers, _ := s.store.ListServers(ctx)

	// Collect Docker information if available
	var dockerInfo *v1.DockerInfo
	if s.docker != nil {
		// Get Docker version and info
		dockerClient := s.docker.GetDockerClient()
		if dockerClient != nil {
			info, err := dockerClient.Info(ctx)
			if err == nil {
				dockerInfo = &v1.DockerInfo{
					Version:     info.ServerVersion,
					Containers:  int32(info.Containers),
					Images:      int32(info.Images),
					MemoryLimit: info.MemTotal,
					Cpus:        int32(info.NCPU),
				}
			}
		}
	}

	// Build config info
	configInfo := &v1.ConfigInfo{
		StorageDir:     s.config.Storage.DataDir,
		TempDir:        s.config.Storage.TempDir,
		DatabasePath:   s.config.Database.Path,
		ProxyEnabled:   s.config.Proxy.Enabled,
		ProxyPorts:     toInt32Slice(s.config.Proxy.ListenPorts),
		ServerHost:     s.config.Server.Host,
		ServerPort:     s.config.Server.Port,
		LoggingEnabled: s.config.Logging.Enabled,
		LogFilePath:    s.config.Logging.FilePath,
		LogMaxSize:     int32(s.config.Logging.MaxSize),
	}

	// Build proxy config if enabled
	var proxyConfigInfo *v1.ProxyConfigInfo
	if s.config.Proxy.Enabled {
		proxyConfig, _, err := s.store.GetProxyConfig(ctx)
		if err == nil {
			proxyConfigInfo = &v1.ProxyConfigInfo{
				Enabled: proxyConfig.Enabled,
				BaseUrl: proxyConfig.BaseURL,
			}

			// Get proxy listeners
			listeners, err := s.store.GetProxyListeners(ctx)
			if err == nil {
				for _, listener := range listeners {
					proxyConfigInfo.Listeners = append(proxyConfigInfo.Listeners, &v1.ProxyListenerInfo{
						Name:      listener.Name,
						Port:      int32(listener.Port),
						Enabled:   listener.Enabled,
						IsDefault: listener.IsDefault,
					})
				}
			}
		}
	}

	// Build server summaries
	serverSummaries := make([]*v1.ServerSummary, 0, len(servers))
	for _, server := range servers {
		serverSummaries = append(serverSummaries, &v1.ServerSummary{
			Id:          server.ID,
			Name:        server.Name,
			ModLoader:   string(server.ModLoader),
			McVersion:   server.MCVersion,
			Status:      string(server.Status),
			Port:        int32(server.Port),
			Memory:      int32(server.Memory),
			AutoStart:   server.AutoStart,
			DockerImage: server.DockerImage,
		})
	}

	// Build the complete system info
	systemInfo := &v1.SystemInfo{
		Timestamp:   time.Now().Format(time.RFC3339),
		Version:     getVersionInfo(),
		ServerCount: int32(len(servers)),
		Config:      configInfo,
		Docker:      dockerInfo,
		ProxyConfig: proxyConfigInfo,
		Servers:     serverSummaries,
	}

	jsonData, err := json.MarshalIndent(systemInfo, "", "  ")
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

// addFileToTar adds a file to the tar archive
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

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func toInt32Slice(intSlice []int) []int32 {
	result := make([]int32, len(intSlice))
	for i, v := range intSlice {
		result[i] = int32(v)
	}
	return result
}

// getVersionInfo gets version information for the application
func getVersionInfo() string {
	// Check env var first
	if appV := os.Getenv("APP_VERSION"); appV != "" {
		return appV
	}

	// Check version file stored in home
	if home, err := os.UserHomeDir(); err == nil {
		versionFile := filepath.Join(home, ".discopanel")
		if data, err := os.ReadFile(versionFile); err == nil {
			if v := strings.TrimSpace(string(data)); v != "" {
				return v
			}
		}
	}

	info, err := buildinfo.ReadFile(os.Args[0])
	if err != nil {
		return "unknown"
	}

	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}

	return "unknown"
}

// GetApplicationLogs returns the application log file content
func (s *SupportService) GetApplicationLogs(ctx context.Context, req *connect.Request[v1.GetApplicationLogsRequest]) (*connect.Response[v1.GetApplicationLogsResponse], error) {
	logFilePath := s.log.GetLogFilePath()
	if logFilePath == "" {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("logging to file is not enabled"))
	}

	if !fileExists(logFilePath) {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("log file not found"))
	}

	// Get file info for size
	fileInfo, err := os.Stat(logFilePath)
	if err != nil {
		s.log.Error("Failed to stat log file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get log file info"))
	}

	// Read log file content
	content, err := os.ReadFile(logFilePath)
	if err != nil {
		s.log.Error("Failed to read log file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read log file"))
	}

	// If tail is specified, only return the last N lines
	tail := int(req.Msg.Tail)
	if tail > 0 {
		lines := strings.Split(string(content), "\n")
		if len(lines) > tail {
			lines = lines[len(lines)-tail:]
		}
		content = []byte(strings.Join(lines, "\n"))
	}

	return connect.NewResponse(&v1.GetApplicationLogsResponse{
		Content:  string(content),
		Filename: filepath.Base(logFilePath),
		Size:     fileInfo.Size(),
	}), nil
}

// StreamApplicationLogs streams application log updates in real-time
func (s *SupportService) StreamApplicationLogs(ctx context.Context, req *connect.Request[v1.StreamApplicationLogsRequest], stream *connect.ServerStream[v1.StreamApplicationLogsResponse]) error {
	logFilePath := s.log.GetLogFilePath()
	if logFilePath == "" {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("logging to file is not enabled"))
	}

	// Send initial backfill
	content, err := os.ReadFile(logFilePath)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read log file"))
	}

	tail := int(req.Msg.Tail)
	if tail > 0 {
		lines := strings.Split(string(content), "\n")
		if len(lines) > tail {
			lines = lines[len(lines)-tail:]
		}
		content = []byte(strings.Join(lines, "\n"))
	}

	fileInfo, _ := os.Stat(logFilePath)
	if err := stream.Send(&v1.StreamApplicationLogsResponse{
		Content:  string(content),
		Filename: filepath.Base(logFilePath),
		Size:     fileInfo.Size(),
	}); err != nil {
		return err
	}

	// Track file size for incremental reads
	lastOffset := fileInfo.Size()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			fi, err := os.Stat(logFilePath)
			if err != nil {
				continue
			}
			if fi.Size() > lastOffset {
				f, err := os.Open(logFilePath)
				if err != nil {
					continue
				}
				if _, err := f.Seek(lastOffset, io.SeekStart); err != nil {
					f.Close()
					continue
				}
				newContent, err := io.ReadAll(f)
				f.Close()
				if err != nil {
					continue
				}
				lastOffset = fi.Size()
				if len(newContent) > 0 {
					if err := stream.Send(&v1.StreamApplicationLogsResponse{
						Content:  string(newContent),
						Filename: filepath.Base(logFilePath),
						Size:     fi.Size(),
					}); err != nil {
						return err
					}
				}
			}
		}
	}
}
