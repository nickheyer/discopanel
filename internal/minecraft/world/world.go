package world

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickheyer/discopanel/internal/minecraft/nbt"
)

// WorldInfo contains metadata about a Minecraft world
type WorldInfo struct {
	Name          string
	Path          string
	LevelName     string
	GameType      int
	Hardcore      bool
	Seed          int64
	SpawnX        int
	SpawnY        int
	SpawnZ        int
	GeneratorName string
	DataVersion   int
	Dimensions    []string
	MinY          int
	MaxY          int
}

// World provides access to a Minecraft world
type World struct {
	Path     string
	Info     *WorldInfo
	managers map[string]*RegionManager
}

// OpenWorld opens a Minecraft world directory
func OpenWorld(path string) (*World, error) {
	info, err := ReadWorldInfo(path)
	if err != nil {
		return nil, fmt.Errorf("reading world info: %w", err)
	}

	return &World{
		Path:     path,
		Info:     info,
		managers: make(map[string]*RegionManager),
	}, nil
}

// ReadWorldInfo reads the level.dat file and extracts world information
func ReadWorldInfo(worldPath string) (*WorldInfo, error) {
	levelPath := filepath.Join(worldPath, "level.dat")

	data, err := os.ReadFile(levelPath)
	if err != nil {
		return nil, fmt.Errorf("reading level.dat: %w", err)
	}

	tag, err := nbt.Read(data)
	if err != nil {
		return nil, fmt.Errorf("parsing level.dat NBT: %w", err)
	}

	if tag.Type != nbt.TagCompound {
		return nil, fmt.Errorf("level.dat root is not a compound")
	}

	root := tag.Value.(nbt.Compound)

	// The actual data is in the "Data" compound
	dataCompound, ok := root.GetCompound("Data")
	if !ok {
		return nil, fmt.Errorf("level.dat missing Data compound")
	}

	info := &WorldInfo{
		Path: worldPath,
		MinY: -64,
		MaxY: 320,
	}

	// Parse basic info
	if name, ok := dataCompound.GetString("LevelName"); ok {
		info.LevelName = name
		info.Name = name
	} else {
		info.Name = filepath.Base(worldPath)
	}

	if gameType, ok := dataCompound.GetInt("GameType"); ok {
		info.GameType = int(gameType)
	}

	if hardcore, ok := dataCompound.GetByte("hardcore"); ok {
		info.Hardcore = hardcore != 0
	}

	if dataVersion, ok := dataCompound.GetInt("DataVersion"); ok {
		info.DataVersion = int(dataVersion)

		// Adjust world bounds based on version
		if info.DataVersion < 2724 { // Pre-1.17
			info.MinY = 0
			info.MaxY = 256
		}
	}

	// Parse seed - can be in different places depending on version
	if worldGenSettings, ok := dataCompound.GetCompound("WorldGenSettings"); ok {
		if seed, ok := worldGenSettings.GetLong("seed"); ok {
			info.Seed = seed
		}
	} else if seed, ok := dataCompound.GetLong("RandomSeed"); ok {
		info.Seed = seed
	}

	// Parse spawn
	if x, ok := dataCompound.GetInt("SpawnX"); ok {
		info.SpawnX = int(x)
	}
	if y, ok := dataCompound.GetInt("SpawnY"); ok {
		info.SpawnY = int(y)
	}
	if z, ok := dataCompound.GetInt("SpawnZ"); ok {
		info.SpawnZ = int(z)
	}

	// Parse generator name
	if gen, ok := dataCompound.GetString("generatorName"); ok {
		info.GeneratorName = gen
	} else if worldGenSettings, ok := dataCompound.GetCompound("WorldGenSettings"); ok {
		if dims, ok := worldGenSettings.GetCompound("dimensions"); ok {
			for name := range dims {
				if strings.Contains(name, "overworld") {
					info.GeneratorName = "default"
					break
				}
			}
		}
	}

	// Detect available dimensions
	info.Dimensions = detectDimensions(worldPath)

	return info, nil
}

func detectDimensions(worldPath string) []string {
	dims := []string{"overworld"}

	// Check for nether
	netherPath := filepath.Join(worldPath, "DIM-1", "region")
	if info, err := os.Stat(netherPath); err == nil && info.IsDir() {
		dims = append(dims, "the_nether")
	}

	// Check for end
	endPath := filepath.Join(worldPath, "DIM1", "region")
	if info, err := os.Stat(endPath); err == nil && info.IsDir() {
		dims = append(dims, "the_end")
	}

	return dims
}

// GetManager returns a region manager for the specified dimension
func (w *World) GetManager(dimension string) *RegionManager {
	if manager, ok := w.managers[dimension]; ok {
		return manager
	}

	manager := NewRegionManager(w.Path, dimension)
	w.managers[dimension] = manager
	return manager
}

// GetChunk reads a chunk from the world
func (w *World) GetChunk(dimension string, chunkX, chunkZ int) (*Chunk, error) {
	manager := w.GetManager(dimension)
	nbtData, err := manager.GetChunk(chunkX, chunkZ)
	if err != nil {
		return nil, err
	}

	return ParseChunk(nbtData)
}

// GetChunks reads multiple chunks around a center point
func (w *World) GetChunks(dimension string, centerX, centerZ, radius int) ([]*Chunk, error) {
	var chunks []*Chunk

	for x := centerX - radius; x <= centerX+radius; x++ {
		for z := centerZ - radius; z <= centerZ+radius; z++ {
			chunk, err := w.GetChunk(dimension, x, z)
			if err != nil {
				continue // Skip missing chunks
			}
			chunks = append(chunks, chunk)
		}
	}

	return chunks, nil
}

// WriteChunk writes a chunk to the world
func (w *World) WriteChunk(dimension string, chunk *Chunk) error {
	manager := w.GetManager(dimension)
	nbtData := chunk.ToNBT()
	return manager.WriteChunk(chunk.X, chunk.Z, nbtData)
}

// Close closes all open region managers
func (w *World) Close() error {
	var lastErr error
	for _, manager := range w.managers {
		if err := manager.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// ListWorlds lists all worlds in a server data directory
func ListWorlds(serverDataPath string) ([]*WorldInfo, error) {
	var worlds []*WorldInfo

	entries, err := os.ReadDir(serverDataPath)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		worldPath := filepath.Join(serverDataPath, entry.Name())
		levelPath := filepath.Join(worldPath, "level.dat")

		// Check if this is a world directory
		if _, err := os.Stat(levelPath); err != nil {
			continue
		}

		info, err := ReadWorldInfo(worldPath)
		if err != nil {
			continue
		}

		worlds = append(worlds, info)
	}

	return worlds, nil
}

// GameTypeString returns a human-readable game type name
func GameTypeString(gameType int) string {
	switch gameType {
	case 0:
		return "Survival"
	case 1:
		return "Creative"
	case 2:
		return "Adventure"
	case 3:
		return "Spectator"
	default:
		return "Unknown"
	}
}
