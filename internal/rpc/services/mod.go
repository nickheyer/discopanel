package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that ModService implements the interface
var _ discopanelv1connect.ModServiceHandler = (*ModService)(nil)

// ModService implements the Mod service
type ModService struct {
	store  *storage.Store
	docker *docker.Client
	log    *logger.Logger
}

// NewModService creates a new mod service
func NewModService(store *storage.Store, docker *docker.Client, log *logger.Logger) *ModService {
	return &ModService{
		store:  store,
		docker: docker,
		log:    log,
	}
}

// ListMods lists mods for a server
func (s *ModService) ListMods(ctx context.Context, req *connect.Request[v1.ListModsRequest]) (*connect.Response[v1.ListModsResponse], error) {
	serverID := req.Msg.ServerId
	if serverID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("server_id is required"))
	}

	// Get server to find data path and mod loader
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Get the mods directory path
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("this server type does not support mods"))
	}

	// Check if mods directory exists
	if _, err := os.Stat(modsDir); os.IsNotExist(err) {
		// Return empty list if directory doesn't exist
		return connect.NewResponse(&v1.ListModsResponse{
			Mods: []*v1.Mod{},
		}), nil
	}

	// Read mods from directory
	files, err := os.ReadDir(modsDir)
	if err != nil {
		s.log.Error("Failed to read mods directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to read mods directory"))
	}

	// Build list of mods from files
	mods := []*v1.Mod{}
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Check if this is a valid mod file
		if !minecraft.IsValidModFile(file.Name(), server.ModLoader) {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		// Create mod entry with consistent ID generation
		modID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(serverID+file.Name())).String()

		// Extract mod name from filename (remove extension)
		baseName := file.Name()
		if ext := filepath.Ext(baseName); ext != "" {
			baseName = baseName[:len(baseName)-len(ext)]
		}

		mod := &v1.Mod{
			Id:          modID,
			ServerId:    serverID,
			DisplayName: baseName,
			FileName:    file.Name(),
			Enabled:     true,
			FileSize:    info.Size(),
			UploadedAt:  timestamppb.New(info.ModTime()),
		}

		mods = append(mods, mod)
	}

	// Also check disabled mods directory
	disabledDir := modsDir + "_disabled"
	if files, err := os.ReadDir(disabledDir); err == nil {
		for _, file := range files {
			if file.IsDir() {
				continue
			}

			if !minecraft.IsValidModFile(file.Name(), server.ModLoader) {
				continue
			}

			info, err := file.Info()
			if err != nil {
				continue
			}

			modID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(serverID+file.Name())).String()

			// Extract mod name from filename
			baseName := file.Name()
			if ext := filepath.Ext(baseName); ext != "" {
				baseName = baseName[:len(baseName)-len(ext)]
			}

			mod := &v1.Mod{
				Id:          modID,
				ServerId:    serverID,
				DisplayName: baseName,
				FileName:    file.Name(),
				Enabled:     false,
				FileSize:    info.Size(),
				UploadedAt:  timestamppb.New(info.ModTime()),
			}

			mods = append(mods, mod)
		}
	}

	return connect.NewResponse(&v1.ListModsResponse{
		Mods: mods,
	}), nil
}

// GetMod gets a specific mod
func (s *ModService) GetMod(ctx context.Context, req *connect.Request[v1.GetModRequest]) (*connect.Response[v1.GetModResponse], error) {
	modID := req.Msg.ModId
	if modID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("mod_id is required"))
	}

	mod, err := s.store.GetMod(ctx, modID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("mod not found"))
	}

	return connect.NewResponse(&v1.GetModResponse{
		Mod: dbModToProto(mod),
	}), nil
}

// UploadMod uploads a new mod
func (s *ModService) UploadMod(ctx context.Context, req *connect.Request[v1.UploadModRequest]) (*connect.Response[v1.UploadModResponse], error) {
	serverID := req.Msg.ServerId
	if serverID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("server_id is required"))
	}

	if req.Msg.Filename == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("filename is required"))
	}

	if len(req.Msg.Content) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no mod file content provided"))
	}

	// Get server to find data path and mod loader
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Validate file is appropriate for this mod loader
	if !minecraft.IsValidModFile(req.Msg.Filename, server.ModLoader) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid file type for this mod loader"))
	}

	// Get the correct mods directory based on mod loader
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("this server type does not support mods"))
	}

	if err := os.MkdirAll(modsDir, 0755); err != nil {
		s.log.Error("Failed to create mods directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create mods directory"))
	}

	// Save file
	modPath := filepath.Join(modsDir, req.Msg.Filename)
	dst, err := os.Create(modPath)
	if err != nil {
		s.log.Error("Failed to create mod file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to save mod"))
	}
	defer dst.Close()

	// Write the bytes directly
	written, err := dst.Write(req.Msg.Content)
	if err != nil {
		s.log.Error("Failed to write mod file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to save mod"))
	}

	// Create mod record
	displayName := req.Msg.DisplayName
	if displayName == "" {
		displayName = req.Msg.Filename
	}

	mod := &storage.Mod{
		ID:          uuid.New().String(),
		ServerID:    serverID,
		Name:        displayName,
		FileName:    req.Msg.Filename,
		Description: req.Msg.Description,
		Enabled:     true,
		FileSize:    int64(written),
	}

	// Save to database
	if err := s.store.AddMod(ctx, mod); err != nil {
		s.log.Error("Failed to save mod record: %v", err)
		os.Remove(modPath) // Clean up file
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to save mod"))
	}

	return connect.NewResponse(&v1.UploadModResponse{
		Mod:     dbModToProto(mod),
		Message: "Mod uploaded successfully",
	}), nil
}

// UpdateMod updates a mod
func (s *ModService) UpdateMod(ctx context.Context, req *connect.Request[v1.UpdateModRequest]) (*connect.Response[v1.UpdateModResponse], error) {
	serverID := req.Msg.ServerId
	modID := req.Msg.ModId

	if serverID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("server_id is required"))
	}

	if modID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("mod_id is required"))
	}

	// Get server to find mod path
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// For now, we only support enabling/disabling
	if req.Msg.Enabled != nil {
		modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
		disabledDir := modsDir + "_disabled"

		// Try to find the mod file by checking both directories
		var modFileName string
		var currentlyEnabled bool

		// First, scan the mods directory
		if files, err := os.ReadDir(modsDir); err == nil {
			for _, file := range files {
				if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
					// Generate the same ID as in listMods to match
					fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(serverID+file.Name())).String()
					if fileID == modID {
						modFileName = file.Name()
						currentlyEnabled = true
						break
					}
				}
			}
		}

		// If not found, check disabled directory
		if modFileName == "" {
			if files, err := os.ReadDir(disabledDir); err == nil {
				for _, file := range files {
					if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
						fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(serverID+file.Name())).String()
						if fileID == modID {
							modFileName = file.Name()
							currentlyEnabled = false
							break
						}
					}
				}
			}
		}

		if modFileName == "" {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("mod not found"))
		}

		// Now move the file if needed
		if *req.Msg.Enabled != currentlyEnabled {
			if *req.Msg.Enabled {
				// Move from disabled to mods directory
				oldPath := filepath.Join(disabledDir, modFileName)
				newPath := filepath.Join(modsDir, modFileName)
				if err := os.Rename(oldPath, newPath); err != nil {
					s.log.Error("Failed to enable mod: %v", err)
					return nil, connect.NewError(connect.CodeInternal, errors.New("failed to enable mod"))
				}
			} else {
				// Move from mods to disabled directory
				os.MkdirAll(disabledDir, 0755)
				oldPath := filepath.Join(modsDir, modFileName)
				newPath := filepath.Join(disabledDir, modFileName)
				if err := os.Rename(oldPath, newPath); err != nil {
					s.log.Error("Failed to disable mod: %v", err)
					return nil, connect.NewError(connect.CodeInternal, errors.New("failed to disable mod"))
				}
			}
		}

		// Get file info for response
		var finalPath string
		if *req.Msg.Enabled {
			finalPath = filepath.Join(modsDir, modFileName)
		} else {
			finalPath = filepath.Join(disabledDir, modFileName)
		}

		info, err := os.Stat(finalPath)
		if err != nil {
			s.log.Error("Failed to get mod file info: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get mod info"))
		}

		// Extract mod name from filename
		baseName := modFileName
		if ext := filepath.Ext(baseName); ext != "" {
			baseName = baseName[:len(baseName)-len(ext)]
		}

		// Create mod response
		mod := &v1.Mod{
			Id:          modID,
			ServerId:    serverID,
			DisplayName: baseName,
			FileName:    modFileName,
			Enabled:     *req.Msg.Enabled,
			FileSize:    info.Size(),
			UploadedAt:  timestamppb.New(info.ModTime()),
		}

		// Apply optional display name and description if provided
		if req.Msg.DisplayName != nil {
			mod.DisplayName = *req.Msg.DisplayName
		}
		if req.Msg.Description != nil {
			mod.Description = *req.Msg.Description
		}

		return connect.NewResponse(&v1.UpdateModResponse{
			Mod: mod,
		}), nil
	}

	// If only updating metadata (display name or description), we need to update the database
	// However, the original implementation doesn't handle this, so we'll just return success
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("only enabled/disabled updates are currently supported"))
}

// DeleteMod deletes a mod
func (s *ModService) DeleteMod(ctx context.Context, req *connect.Request[v1.DeleteModRequest]) (*connect.Response[v1.DeleteModResponse], error) {
	serverID := req.Msg.ServerId
	modID := req.Msg.ModId

	if serverID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("server_id is required"))
	}

	if modID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("mod_id is required"))
	}

	// Get server to find mod path
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	disabledDir := modsDir + "_disabled"

	// Try to find and delete the mod file from both directories
	var modFileName string
	var deleted bool

	// First, scan the mods directory
	if files, err := os.ReadDir(modsDir); err == nil {
		for _, file := range files {
			if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
				// Generate the same ID as in listMods to match
				fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(serverID+file.Name())).String()
				if fileID == modID {
					modFileName = file.Name()
					modPath := filepath.Join(modsDir, modFileName)
					if err := os.Remove(modPath); err != nil {
						s.log.Error("Failed to delete mod file: %v", err)
					} else {
						deleted = true
					}
					break
				}
			}
		}
	}

	// If not found in active directory, check disabled directory
	if modFileName == "" {
		if files, err := os.ReadDir(disabledDir); err == nil {
			for _, file := range files {
				if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
					fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(serverID+file.Name())).String()
					if fileID == modID {
						modFileName = file.Name()
						disabledPath := filepath.Join(disabledDir, modFileName)
						if err := os.Remove(disabledPath); err != nil {
							s.log.Error("Failed to delete disabled mod file: %v", err)
						} else {
							deleted = true
						}
						break
					}
				}
			}
		}
	}

	if !deleted && modFileName == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("mod not found"))
	}

	// Try to delete from database (may not exist if using file-based approach)
	if err := s.store.DeleteMod(ctx, modID); err != nil {
		s.log.Error("Failed to delete mod from database: %v", err)
		// Don't return error, as file-based deletion succeeded
	}

	return connect.NewResponse(&v1.DeleteModResponse{
		Message: "Mod deleted successfully",
	}), nil
}

// dbModToProto converts a database Mod to a protobuf Mod
func dbModToProto(mod *storage.Mod) *v1.Mod {
	if mod == nil {
		return nil
	}

	protoMod := &v1.Mod{
		Id:          mod.ID,
		ServerId:    mod.ServerID,
		FileName:    mod.FileName,
		DisplayName: mod.Name,
		Description: mod.Description,
		Version:     mod.Version,
		ModId:       mod.ModID,
		FileSize:    mod.FileSize,
		Enabled:     mod.Enabled,
		UploadedAt:  timestamppb.New(mod.UploadedAt),
	}

	return protoMod
}
