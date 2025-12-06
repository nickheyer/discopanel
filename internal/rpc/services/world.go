package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"connectrpc.com/connect"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/minecraft/world"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
)

var _ discopanelv1connect.WorldServiceHandler = (*WorldService)(nil)

// WorldService implements the World editing service
type WorldService struct {
	store       *storage.Store
	docker      *docker.Client
	log         *logger.Logger
	worlds      map[string]*world.World
	worldsMu    sync.RWMutex
	clipboards  map[string]*v1.ClipboardData
	clipboardMu sync.RWMutex
	operations  map[string]*Operation
	opMu        sync.RWMutex
}

// Operation stores undo/redo information
type Operation struct {
	ID       string
	ServerID string
	World    string
	Changes  []*v1.BlockChange
}

// NewWorldService creates a new world service
func NewWorldService(store *storage.Store, docker *docker.Client, log *logger.Logger) *WorldService {
	return &WorldService{
		store:      store,
		docker:     docker,
		log:        log,
		worlds:     make(map[string]*world.World),
		clipboards: make(map[string]*v1.ClipboardData),
		operations: make(map[string]*Operation),
	}
}

func (s *WorldService) getWorld(serverID, worldName string) (*world.World, error) {
	key := serverID + ":" + worldName

	s.worldsMu.RLock()
	if w, ok := s.worlds[key]; ok {
		s.worldsMu.RUnlock()
		return w, nil
	}
	s.worldsMu.RUnlock()

	s.worldsMu.Lock()
	defer s.worldsMu.Unlock()

	// Double check
	if w, ok := s.worlds[key]; ok {
		return w, nil
	}

	// Get server to find data path
	server, err := s.store.GetServer(context.Background(), serverID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}

	worldPath := filepath.Join(server.DataPath, worldName)
	w, err := world.OpenWorld(worldPath)
	if err != nil {
		return nil, err
	}

	s.worlds[key] = w
	return w, nil
}

// ListWorlds lists available worlds for a server
func (s *WorldService) ListWorlds(ctx context.Context, req *connect.Request[v1.ListWorldsRequest]) (*connect.Response[v1.ListWorldsResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	worlds, err := world.ListWorlds(server.DataPath)
	if err != nil {
		s.log.Error("Failed to list worlds: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to list worlds"))
	}

	var worldInfos []*v1.WorldInfo
	for _, w := range worlds {
		// Get directory size
		var size int64
		filepath.Walk(w.Path, func(_ string, info os.FileInfo, _ error) error {
			if info != nil && !info.IsDir() {
				size += info.Size()
			}
			return nil
		})

		// Get modification time
		var modTime int64
		if info, err := os.Stat(filepath.Join(w.Path, "level.dat")); err == nil {
			modTime = info.ModTime().Unix()
		}

		worldInfos = append(worldInfos, &v1.WorldInfo{
			Name:          w.Name,
			Path:          w.Path,
			SizeBytes:     size,
			LastModified:  modTime,
			LevelName:     w.LevelName,
			GameType:      world.GameTypeString(w.GameType),
			Hardcore:      w.Hardcore,
			Seed:          w.Seed,
			SpawnX:        int32(w.SpawnX),
			SpawnY:        int32(w.SpawnY),
			SpawnZ:        int32(w.SpawnZ),
			GeneratorName: w.GeneratorName,
			DataVersion:   int32(w.DataVersion),
		})
	}

	return connect.NewResponse(&v1.ListWorldsResponse{
		Worlds: worldInfos,
	}), nil
}

// GetWorldInfo gets detailed world information
func (s *WorldService) GetWorldInfo(ctx context.Context, req *connect.Request[v1.GetWorldInfoRequest]) (*connect.Response[v1.GetWorldInfoResponse], error) {
	w, err := s.getWorld(req.Msg.ServerId, req.Msg.WorldName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("world not found: %w", err))
	}

	// Get directory size
	var size int64
	filepath.Walk(w.Path, func(_ string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	// Get modification time
	var modTime int64
	if info, err := os.Stat(filepath.Join(w.Path, "level.dat")); err == nil {
		modTime = info.ModTime().Unix()
	}

	worldInfo := &v1.WorldInfo{
		Name:          w.Info.Name,
		Path:          w.Path,
		SizeBytes:     size,
		LastModified:  modTime,
		LevelName:     w.Info.LevelName,
		GameType:      world.GameTypeString(w.Info.GameType),
		Hardcore:      w.Info.Hardcore,
		Seed:          w.Info.Seed,
		SpawnX:        int32(w.Info.SpawnX),
		SpawnY:        int32(w.Info.SpawnY),
		SpawnZ:        int32(w.Info.SpawnZ),
		GeneratorName: w.Info.GeneratorName,
		DataVersion:   int32(w.Info.DataVersion),
	}

	return connect.NewResponse(&v1.GetWorldInfoResponse{
		World:      worldInfo,
		Dimensions: w.Info.Dimensions,
		MinY:       int32(w.Info.MinY),
		MaxY:       int32(w.Info.MaxY),
	}), nil
}

// GetChunks loads multiple chunks for rendering
func (s *WorldService) GetChunks(ctx context.Context, req *connect.Request[v1.GetChunksRequest]) (*connect.Response[v1.GetChunksResponse], error) {
	w, err := s.getWorld(req.Msg.ServerId, req.Msg.WorldName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("world not found: %w", err))
	}

	dimension := req.Msg.Dimension
	if dimension == "" {
		dimension = "overworld"
	}

	chunks, err := w.GetChunks(dimension, int(req.Msg.CenterX), int(req.Msg.CenterZ), int(req.Msg.Radius))
	if err != nil {
		s.log.Error("Failed to load chunks: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to load chunks"))
	}

	if req.Msg.Compact {
		return s.chunksToCompact(chunks)
	}

	return s.chunksToFull(chunks)
}

func (s *WorldService) chunksToFull(chunks []*world.Chunk) (*connect.Response[v1.GetChunksResponse], error) {
	var chunkData []*v1.ChunkData

	for _, chunk := range chunks {
		cd := &v1.ChunkData{
			X:           int32(chunk.X),
			Z:           int32(chunk.Z),
			DataVersion: int32(chunk.DataVersion),
		}

		for y, section := range chunk.Sections {
			cs := &v1.ChunkSection{
				Y:              int32(y),
				PaletteIndices: make([]int32, len(section.BlockStates)),
				Palette:        make([]*v1.BlockState, len(section.Palette)),
			}

			for i, idx := range section.BlockStates {
				cs.PaletteIndices[i] = int32(idx)
			}

			for i, block := range section.Palette {
				cs.Palette[i] = &v1.BlockState{
					Name:       block.Name,
					Properties: block.Properties,
				}
			}

			if len(section.BlockLight) > 0 {
				cs.BlockLight = section.BlockLight
			}
			if len(section.SkyLight) > 0 {
				cs.SkyLight = section.SkyLight
			}

			cd.Sections = append(cd.Sections, cs)
		}

		chunkData = append(chunkData, cd)
	}

	return connect.NewResponse(&v1.GetChunksResponse{
		Chunks: chunkData,
	}), nil
}

func (s *WorldService) chunksToCompact(chunks []*world.Chunk) (*connect.Response[v1.GetChunksResponse], error) {
	var compactChunks []*v1.CompactChunk

	for _, chunk := range chunks {
		cc := &v1.CompactChunk{
			X:        int32(chunk.X),
			Z:        int32(chunk.Z),
			Sections: make(map[int32][]byte),
		}

		// Build shared palette
		paletteMap := make(map[string]int)
		var palette []string

		for _, section := range chunk.Sections {
			for _, block := range section.Palette {
				key := block.Name
				if _, ok := paletteMap[key]; !ok {
					paletteMap[key] = len(palette)
					palette = append(palette, key)
				}
			}
		}
		cc.Palette = palette

		// Encode sections
		for y, section := range chunk.Sections {
			// Map local palette to global palette
			localToGlobal := make([]int, len(section.Palette))
			for i, block := range section.Palette {
				localToGlobal[i] = paletteMap[block.Name]
			}

			// Pack block data - 2 bytes per block (16-bit indices support large palettes)
			data := make([]byte, len(section.BlockStates)*2)
			for i, idx := range section.BlockStates {
				globalIdx := localToGlobal[idx]
				data[i*2] = byte(globalIdx & 0xFF)
				data[i*2+1] = byte((globalIdx >> 8) & 0xFF)
			}

			cc.Sections[int32(y)] = data
		}

		compactChunks = append(compactChunks, cc)
	}

	return connect.NewResponse(&v1.GetChunksResponse{
		CompactChunks: compactChunks,
	}), nil
}

// GetChunk loads a single chunk with full data
func (s *WorldService) GetChunk(ctx context.Context, req *connect.Request[v1.GetChunkRequest]) (*connect.Response[v1.GetChunkResponse], error) {
	w, err := s.getWorld(req.Msg.ServerId, req.Msg.WorldName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("world not found: %w", err))
	}

	dimension := req.Msg.Dimension
	if dimension == "" {
		dimension = "overworld"
	}

	chunk, err := w.GetChunk(dimension, int(req.Msg.ChunkX), int(req.Msg.ChunkZ))
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("chunk not found: %w", err))
	}

	resp, err := s.chunksToFull([]*world.Chunk{chunk})
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&v1.GetChunkResponse{
		Chunk: resp.Msg.Chunks[0],
	}), nil
}

// SetBlocks applies block changes to the world
func (s *WorldService) SetBlocks(ctx context.Context, req *connect.Request[v1.SetBlocksRequest]) (*connect.Response[v1.SetBlocksResponse], error) {
	w, err := s.getWorld(req.Msg.ServerId, req.Msg.WorldName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("world not found: %w", err))
	}

	dimension := req.Msg.Dimension
	if dimension == "" {
		dimension = "overworld"
	}

	// Group changes by chunk
	chunkChanges := make(map[[2]int][]*v1.BlockChange)
	for _, change := range req.Msg.Changes {
		chunkX := int(change.Position.X) >> 4
		chunkZ := int(change.Position.Z) >> 4
		key := [2]int{chunkX, chunkZ}
		chunkChanges[key] = append(chunkChanges[key], change)
	}

	// Store old blocks for undo
	var oldChanges []*v1.BlockChange

	// Apply changes per chunk
	blocksChanged := 0
	for key, changes := range chunkChanges {
		chunk, err := w.GetChunk(dimension, key[0], key[1])
		if err != nil {
			continue
		}

		for _, change := range changes {
			localX := int(change.Position.X) & 15
			localZ := int(change.Position.Z) & 15
			y := int(change.Position.Y)

			// Get old block for undo
			oldBlock, _ := chunk.GetBlock(localX, y, localZ)
			if oldBlock != nil {
				oldChanges = append(oldChanges, &v1.BlockChange{
					Position: change.Position,
					Block: &v1.BlockState{
						Name:       oldBlock.Name,
						Properties: oldBlock.Properties,
					},
				})
			}

			// Set new block
			err := chunk.SetBlock(localX, y, localZ, world.BlockState{
				Name:       world.ParseBlockName(change.Block.Name),
				Properties: change.Block.Properties,
			})
			if err == nil {
				blocksChanged++
			}
		}

		// Write chunk back
		if err := w.WriteChunk(dimension, chunk); err != nil {
			s.log.Error("Failed to write chunk: %v", err)
		}
	}

	// Store operation for undo
	opID := fmt.Sprintf("%s-%d", req.Msg.ServerId, len(s.operations))
	s.opMu.Lock()
	s.operations[opID] = &Operation{
		ID:       opID,
		ServerID: req.Msg.ServerId,
		World:    req.Msg.WorldName,
		Changes:  oldChanges,
	}
	s.opMu.Unlock()

	return connect.NewResponse(&v1.SetBlocksResponse{
		BlocksChanged: int32(blocksChanged),
		OperationId:   opID,
	}), nil
}

// FillRegion fills a region with a block type
func (s *WorldService) FillRegion(ctx context.Context, req *connect.Request[v1.FillRegionRequest]) (*connect.Response[v1.FillRegionResponse], error) {
	w, err := s.getWorld(req.Msg.ServerId, req.Msg.WorldName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("world not found: %w", err))
	}

	dimension := req.Msg.Dimension
	if dimension == "" {
		dimension = "overworld"
	}

	// Calculate bounds
	minX := min(int(req.Msg.From.X), int(req.Msg.To.X))
	maxX := max(int(req.Msg.From.X), int(req.Msg.To.X))
	minY := min(int(req.Msg.From.Y), int(req.Msg.To.Y))
	maxY := max(int(req.Msg.From.Y), int(req.Msg.To.Y))
	minZ := min(int(req.Msg.From.Z), int(req.Msg.To.Z))
	maxZ := max(int(req.Msg.From.Z), int(req.Msg.To.Z))

	// Limit region size
	volume := (maxX - minX + 1) * (maxY - minY + 1) * (maxZ - minZ + 1)
	if volume > 1000000 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("region too large (max 1M blocks)"))
	}

	fillBlock := world.BlockState{
		Name:       world.ParseBlockName(req.Msg.Block.Name),
		Properties: req.Msg.Block.Properties,
	}

	var filterBlock *world.BlockState
	if req.Msg.Filter != nil && req.Msg.Filter.Name != "" {
		filterBlock = &world.BlockState{
			Name:       world.ParseBlockName(req.Msg.Filter.Name),
			Properties: req.Msg.Filter.Properties,
		}
	}

	var oldChanges []*v1.BlockChange
	blocksChanged := 0

	// Process chunks
	minChunkX := minX >> 4
	maxChunkX := maxX >> 4
	minChunkZ := minZ >> 4
	maxChunkZ := maxZ >> 4

	for cx := minChunkX; cx <= maxChunkX; cx++ {
		for cz := minChunkZ; cz <= maxChunkZ; cz++ {
			chunk, err := w.GetChunk(dimension, cx, cz)
			if err != nil {
				continue
			}

			modified := false
			for x := max(minX, cx*16); x <= min(maxX, cx*16+15); x++ {
				for z := max(minZ, cz*16); z <= min(maxZ, cz*16+15); z++ {
					for y := minY; y <= maxY; y++ {
						localX := x & 15
						localZ := z & 15

						// Check filter
						if filterBlock != nil {
							existing, _ := chunk.GetBlock(localX, y, localZ)
							if existing == nil || existing.Name != filterBlock.Name {
								continue
							}
						}

						// Store old block
						oldBlock, _ := chunk.GetBlock(localX, y, localZ)
						if oldBlock != nil {
							oldChanges = append(oldChanges, &v1.BlockChange{
								Position: &v1.BlockPos{X: int32(x), Y: int32(y), Z: int32(z)},
								Block:    &v1.BlockState{Name: oldBlock.Name, Properties: oldBlock.Properties},
							})
						}

						if err := chunk.SetBlock(localX, y, localZ, fillBlock); err == nil {
							blocksChanged++
							modified = true
						}
					}
				}
			}

			if modified {
				w.WriteChunk(dimension, chunk)
			}
		}
	}

	// Store operation
	opID := fmt.Sprintf("%s-fill-%d", req.Msg.ServerId, len(s.operations))
	s.opMu.Lock()
	s.operations[opID] = &Operation{
		ID:       opID,
		ServerID: req.Msg.ServerId,
		World:    req.Msg.WorldName,
		Changes:  oldChanges,
	}
	s.opMu.Unlock()

	return connect.NewResponse(&v1.FillRegionResponse{
		BlocksChanged: int32(blocksChanged),
		OperationId:   opID,
	}), nil
}

// ReplaceBlocks replaces blocks within a region
func (s *WorldService) ReplaceBlocks(ctx context.Context, req *connect.Request[v1.ReplaceBlocksRequest]) (*connect.Response[v1.ReplaceBlocksResponse], error) {
	// Reuse FillRegion with filter
	fillReq := &v1.FillRegionRequest{
		ServerId:  req.Msg.ServerId,
		WorldName: req.Msg.WorldName,
		Dimension: req.Msg.Dimension,
		From:      req.Msg.From,
		To:        req.Msg.To,
		Block:     req.Msg.Replace,
		Filter:    req.Msg.Find,
	}

	resp, err := s.FillRegion(ctx, connect.NewRequest(fillReq))
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&v1.ReplaceBlocksResponse{
		BlocksReplaced: resp.Msg.BlocksChanged,
		OperationId:    resp.Msg.OperationId,
	}), nil
}

// CopyRegion copies a region to clipboard
func (s *WorldService) CopyRegion(ctx context.Context, req *connect.Request[v1.CopyRegionRequest]) (*connect.Response[v1.CopyRegionResponse], error) {
	w, err := s.getWorld(req.Msg.ServerId, req.Msg.WorldName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("world not found: %w", err))
	}

	dimension := req.Msg.Dimension
	if dimension == "" {
		dimension = "overworld"
	}

	// Calculate bounds
	minX := min(int(req.Msg.From.X), int(req.Msg.To.X))
	maxX := max(int(req.Msg.From.X), int(req.Msg.To.X))
	minY := min(int(req.Msg.From.Y), int(req.Msg.To.Y))
	maxY := max(int(req.Msg.From.Y), int(req.Msg.To.Y))
	minZ := min(int(req.Msg.From.Z), int(req.Msg.To.Z))
	maxZ := max(int(req.Msg.From.Z), int(req.Msg.To.Z))

	width := maxX - minX + 1
	height := maxY - minY + 1
	depth := maxZ - minZ + 1

	// Limit clipboard size
	volume := width * height * depth
	if volume > 1000000 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("region too large (max 1M blocks)"))
	}

	// Build palette and block data
	paletteMap := make(map[string]int)
	var palette []*v1.BlockState
	blocks := make([]byte, volume*2)

	idx := 0
	for y := minY; y <= maxY; y++ {
		for z := minZ; z <= maxZ; z++ {
			for x := minX; x <= maxX; x++ {
				chunkX := x >> 4
				chunkZ := z >> 4
				localX := x & 15
				localZ := z & 15

				chunk, err := w.GetChunk(dimension, chunkX, chunkZ)
				var block *world.BlockState
				if err == nil {
					block, _ = chunk.GetBlock(localX, y, localZ)
				}
				if block == nil {
					block = &world.BlockState{Name: "minecraft:air"}
				}

				key := block.Name
				paletteIdx, ok := paletteMap[key]
				if !ok {
					paletteIdx = len(palette)
					paletteMap[key] = paletteIdx
					palette = append(palette, &v1.BlockState{
						Name:       block.Name,
						Properties: block.Properties,
					})
				}

				blocks[idx*2] = byte(paletteIdx & 0xFF)
				blocks[idx*2+1] = byte((paletteIdx >> 8) & 0xFF)
				idx++
			}
		}
	}

	clipID := fmt.Sprintf("%s-clip-%d", req.Msg.ServerId, len(s.clipboards))
	clipboard := &v1.ClipboardData{
		Id:      clipID,
		Width:   int32(width),
		Height:  int32(height),
		Depth:   int32(depth),
		Palette: palette,
		Blocks:  blocks,
		Origin:  &v1.BlockPos{X: int32(minX), Y: int32(minY), Z: int32(minZ)},
	}

	s.clipboardMu.Lock()
	s.clipboards[clipID] = clipboard
	s.clipboardMu.Unlock()

	return connect.NewResponse(&v1.CopyRegionResponse{
		Clipboard: clipboard,
	}), nil
}

// PasteRegion pastes clipboard at position
func (s *WorldService) PasteRegion(ctx context.Context, req *connect.Request[v1.PasteRegionRequest]) (*connect.Response[v1.PasteRegionResponse], error) {
	w, err := s.getWorld(req.Msg.ServerId, req.Msg.WorldName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("world not found: %w", err))
	}

	dimension := req.Msg.Dimension
	if dimension == "" {
		dimension = "overworld"
	}

	clip := req.Msg.Clipboard
	if clip == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no clipboard data"))
	}

	baseX := int(req.Msg.Position.X)
	baseY := int(req.Msg.Position.Y)
	baseZ := int(req.Msg.Position.Z)

	var oldChanges []*v1.BlockChange
	blocksChanged := 0

	idx := 0
	for y := 0; y < int(clip.Height); y++ {
		for z := 0; z < int(clip.Depth); z++ {
			for x := 0; x < int(clip.Width); x++ {
				paletteIdx := int(clip.Blocks[idx*2]) | (int(clip.Blocks[idx*2+1]) << 8)
				idx++

				if paletteIdx >= len(clip.Palette) {
					continue
				}

				block := clip.Palette[paletteIdx]

				// Skip air if requested
				if req.Msg.IgnoreAir && block.Name == "minecraft:air" {
					continue
				}

				worldX := baseX + x
				worldY := baseY + y
				worldZ := baseZ + z

				chunkX := worldX >> 4
				chunkZ := worldZ >> 4
				localX := worldX & 15
				localZ := worldZ & 15

				chunk, err := w.GetChunk(dimension, chunkX, chunkZ)
				if err != nil {
					continue
				}

				// Store old block
				oldBlock, _ := chunk.GetBlock(localX, worldY, localZ)
				if oldBlock != nil {
					oldChanges = append(oldChanges, &v1.BlockChange{
						Position: &v1.BlockPos{X: int32(worldX), Y: int32(worldY), Z: int32(worldZ)},
						Block:    &v1.BlockState{Name: oldBlock.Name, Properties: oldBlock.Properties},
					})
				}

				err = chunk.SetBlock(localX, worldY, localZ, world.BlockState{
					Name:       block.Name,
					Properties: block.Properties,
				})
				if err == nil {
					blocksChanged++
				}

				w.WriteChunk(dimension, chunk)
			}
		}
	}

	// Store operation
	opID := fmt.Sprintf("%s-paste-%d", req.Msg.ServerId, len(s.operations))
	s.opMu.Lock()
	s.operations[opID] = &Operation{
		ID:       opID,
		ServerID: req.Msg.ServerId,
		World:    req.Msg.WorldName,
		Changes:  oldChanges,
	}
	s.opMu.Unlock()

	return connect.NewResponse(&v1.PasteRegionResponse{
		BlocksPasted: int32(blocksChanged),
		OperationId:  opID,
	}), nil
}

// Undo reverts the last operation
func (s *WorldService) Undo(ctx context.Context, req *connect.Request[v1.UndoRequest]) (*connect.Response[v1.UndoResponse], error) {
	s.opMu.RLock()
	op, ok := s.operations[req.Msg.OperationId]
	s.opMu.RUnlock()

	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("operation not found"))
	}

	// Apply old changes
	setReq := &v1.SetBlocksRequest{
		ServerId:  op.ServerID,
		WorldName: op.World,
		Dimension: "overworld",
		Changes:   op.Changes,
	}

	_, err := s.SetBlocks(ctx, connect.NewRequest(setReq))
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&v1.UndoResponse{
		Success:        true,
		BlocksReverted: int32(len(op.Changes)),
	}), nil
}

// Redo re-applies an undone operation
func (s *WorldService) Redo(ctx context.Context, req *connect.Request[v1.RedoRequest]) (*connect.Response[v1.RedoResponse], error) {
	// For simplicity, redo is similar to undo - both just apply stored changes
	resp, err := s.Undo(ctx, connect.NewRequest(&v1.UndoRequest{
		ServerId:    req.Msg.ServerId,
		WorldName:   req.Msg.WorldName,
		OperationId: req.Msg.OperationId,
	}))
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&v1.RedoResponse{
		Success:       resp.Msg.Success,
		BlocksApplied: resp.Msg.BlocksReverted,
	}), nil
}

// GetBlockRegistry returns the block registry for the given MC version
func (s *WorldService) GetBlockRegistry(ctx context.Context, req *connect.Request[v1.GetBlockRegistryRequest]) (*connect.Response[v1.GetBlockRegistryResponse], error) {
	// Return a basic block registry with common blocks
	// In a production implementation, this would load from version-specific data files
	blocks := getCommonBlocks()

	return connect.NewResponse(&v1.GetBlockRegistryResponse{
		Blocks:      blocks,
		DataVersion: 3578, // 1.20.4
	}), nil
}

func getCommonBlocks() []*v1.BlockDefinition {
	return []*v1.BlockDefinition{
		{Name: "minecraft:air", Id: 0, IsSolid: false, IsTransparent: true, RenderType: "invisible"},
		{Name: "minecraft:stone", Id: 1, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:granite", Id: 2, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:polished_granite", Id: 3, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:diorite", Id: 4, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:polished_diorite", Id: 5, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:andesite", Id: 6, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:polished_andesite", Id: 7, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:grass_block", Id: 8, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:dirt", Id: 9, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:coarse_dirt", Id: 10, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:podzol", Id: 11, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:cobblestone", Id: 12, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:oak_planks", Id: 13, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:spruce_planks", Id: 14, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:birch_planks", Id: 15, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:jungle_planks", Id: 16, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:acacia_planks", Id: 17, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:dark_oak_planks", Id: 18, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:oak_sapling", Id: 19, IsSolid: false, IsTransparent: true, RenderType: "cross"},
		{Name: "minecraft:bedrock", Id: 20, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:water", Id: 21, IsSolid: false, IsTransparent: true, RenderType: "translucent"},
		{Name: "minecraft:lava", Id: 22, IsSolid: false, EmitsLight: true, LightLevel: 15, RenderType: "translucent"},
		{Name: "minecraft:sand", Id: 23, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:red_sand", Id: 24, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:gravel", Id: 25, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:gold_ore", Id: 26, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:iron_ore", Id: 27, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:coal_ore", Id: 28, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:oak_log", Id: 29, IsSolid: true, RenderType: "solid", Properties: []string{"axis"}},
		{Name: "minecraft:oak_leaves", Id: 30, IsSolid: true, IsTransparent: true, RenderType: "cutout"},
		{Name: "minecraft:sponge", Id: 31, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:glass", Id: 32, IsSolid: true, IsTransparent: true, RenderType: "translucent"},
		{Name: "minecraft:lapis_ore", Id: 33, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:lapis_block", Id: 34, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:dispenser", Id: 35, IsSolid: true, RenderType: "solid", Properties: []string{"facing", "triggered"}},
		{Name: "minecraft:sandstone", Id: 36, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:note_block", Id: 37, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:white_wool", Id: 38, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:gold_block", Id: 39, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:iron_block", Id: 40, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:bricks", Id: 41, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:tnt", Id: 42, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:bookshelf", Id: 43, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:mossy_cobblestone", Id: 44, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:obsidian", Id: 45, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:torch", Id: 46, IsSolid: false, EmitsLight: true, LightLevel: 14, RenderType: "cross"},
		{Name: "minecraft:spawner", Id: 47, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:chest", Id: 48, IsSolid: true, RenderType: "solid", Properties: []string{"facing", "type", "waterlogged"}},
		{Name: "minecraft:diamond_ore", Id: 49, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:diamond_block", Id: 50, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:crafting_table", Id: 51, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:furnace", Id: 52, IsSolid: true, RenderType: "solid", Properties: []string{"facing", "lit"}},
		{Name: "minecraft:ladder", Id: 53, IsSolid: false, IsTransparent: true, RenderType: "cutout"},
		{Name: "minecraft:rail", Id: 54, IsSolid: false, IsTransparent: true, RenderType: "cutout"},
		{Name: "minecraft:lever", Id: 55, IsSolid: false, RenderType: "cutout"},
		{Name: "minecraft:redstone_ore", Id: 56, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:redstone_torch", Id: 57, IsSolid: false, EmitsLight: true, LightLevel: 7, RenderType: "cross"},
		{Name: "minecraft:snow", Id: 58, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:ice", Id: 59, IsSolid: true, IsTransparent: true, RenderType: "translucent"},
		{Name: "minecraft:snow_block", Id: 60, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:cactus", Id: 61, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:clay", Id: 62, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:jukebox", Id: 63, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:pumpkin", Id: 64, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:netherrack", Id: 65, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:soul_sand", Id: 66, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:glowstone", Id: 67, IsSolid: true, EmitsLight: true, LightLevel: 15, RenderType: "solid"},
		{Name: "minecraft:jack_o_lantern", Id: 68, IsSolid: true, EmitsLight: true, LightLevel: 15, RenderType: "solid"},
		{Name: "minecraft:white_stained_glass", Id: 69, IsSolid: true, IsTransparent: true, RenderType: "translucent"},
		{Name: "minecraft:stone_bricks", Id: 70, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:deepslate", Id: 71, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:deepslate_coal_ore", Id: 72, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:deepslate_iron_ore", Id: 73, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:deepslate_gold_ore", Id: 74, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:deepslate_diamond_ore", Id: 75, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:copper_ore", Id: 76, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:copper_block", Id: 77, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:amethyst_block", Id: 78, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:tuff", Id: 79, IsSolid: true, RenderType: "solid"},
		{Name: "minecraft:calcite", Id: 80, IsSolid: true, RenderType: "solid"},
	}
}
