package world

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/nickheyer/discopanel/internal/minecraft/nbt"
)

const (
	regionSize        = 32   // Chunks per region dimension
	sectorSize        = 4096 // Bytes per sector
	headerSize        = 8192 // Location table + timestamp table
	maxChunkSize      = 1024 * 1024
	compressionGzip   = 1
	compressionZlib   = 2
	compressionNone   = 3
	compressionLZ4    = 4
	compressionCustom = 127
)

var (
	ErrChunkNotFound   = errors.New("chunk not found in region")
	ErrInvalidRegion   = errors.New("invalid region file")
	ErrChunkTooLarge   = errors.New("chunk data too large")
	ErrInvalidPosition = errors.New("invalid chunk position")
)

// Region represents a Minecraft region file (32x32 chunks)
type Region struct {
	path       string
	file       *os.File
	mu         sync.RWMutex
	locations  [1024]uint32 // 3 bytes offset + 1 byte sector count
	timestamps [1024]uint32
	dirty      bool
}

// RegionCoord calculates region coordinates from chunk coordinates
func RegionCoord(chunkX, chunkZ int) (int, int) {
	rx := chunkX >> 5
	rz := chunkZ >> 5
	if chunkX < 0 && chunkX&31 != 0 {
		rx--
	}
	if chunkZ < 0 && chunkZ&31 != 0 {
		rz--
	}
	return rx, rz
}

// ChunkIndex calculates the index in the region for a chunk
func ChunkIndex(chunkX, chunkZ int) int {
	return (chunkX & 31) + (chunkZ&31)*32
}

// RegionPath returns the path to a region file
func RegionPath(worldPath, dimension string, rx, rz int) string {
	regionDir := "region"
	if dimension == "nether" || dimension == "the_nether" {
		regionDir = filepath.Join("DIM-1", "region")
	} else if dimension == "end" || dimension == "the_end" {
		regionDir = filepath.Join("DIM1", "region")
	}
	return filepath.Join(worldPath, regionDir, fmt.Sprintf("r.%d.%d.mca", rx, rz))
}

// OpenRegion opens an existing region file
func OpenRegion(path string) (*Region, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening region file: %w", err)
	}

	region := &Region{
		path: path,
		file: file,
	}

	if err := region.readHeader(); err != nil {
		file.Close()
		return nil, err
	}

	return region, nil
}

// CreateRegion creates a new empty region file
func CreateRegion(path string) (*Region, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating region directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("creating region file: %w", err)
	}

	// Write empty header (8KB)
	header := make([]byte, headerSize)
	if _, err := file.Write(header); err != nil {
		file.Close()
		return nil, fmt.Errorf("writing region header: %w", err)
	}

	return &Region{
		path: path,
		file: file,
	}, nil
}

func (r *Region) readHeader() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Read location table
	buf := make([]byte, headerSize)
	if _, err := r.file.ReadAt(buf, 0); err != nil {
		return fmt.Errorf("reading region header: %w", err)
	}

	// Parse locations (first 4096 bytes)
	for i := 0; i < 1024; i++ {
		offset := i * 4
		r.locations[i] = binary.BigEndian.Uint32(buf[offset : offset+4])
	}

	// Parse timestamps (next 4096 bytes)
	for i := 0; i < 1024; i++ {
		offset := 4096 + i*4
		r.timestamps[i] = binary.BigEndian.Uint32(buf[offset : offset+4])
	}

	return nil
}

// HasChunk checks if a chunk exists in the region
func (r *Region) HasChunk(localX, localZ int) bool {
	if localX < 0 || localX >= 32 || localZ < 0 || localZ >= 32 {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	idx := localX + localZ*32
	return r.locations[idx] != 0
}

// ReadChunk reads a chunk's NBT data from the region
func (r *Region) ReadChunk(localX, localZ int) (nbt.Compound, error) {
	if localX < 0 || localX >= 32 || localZ < 0 || localZ >= 32 {
		return nil, ErrInvalidPosition
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	idx := localX + localZ*32
	location := r.locations[idx]

	if location == 0 {
		return nil, ErrChunkNotFound
	}

	// Extract offset and sector count
	offset := int64((location >> 8) & 0xFFFFFF) * sectorSize
	sectorCount := int(location & 0xFF)

	if offset < headerSize {
		return nil, ErrInvalidRegion
	}

	// Read chunk data header (4 bytes length + 1 byte compression)
	header := make([]byte, 5)
	if _, err := r.file.ReadAt(header, offset); err != nil {
		return nil, fmt.Errorf("reading chunk header: %w", err)
	}

	length := binary.BigEndian.Uint32(header[0:4])
	compression := header[4]

	if length == 0 || int(length) > sectorCount*sectorSize {
		return nil, ErrInvalidRegion
	}

	// Read compressed chunk data
	data := make([]byte, length-1)
	if _, err := r.file.ReadAt(data, offset+5); err != nil {
		return nil, fmt.Errorf("reading chunk data: %w", err)
	}

	// Decompress
	var reader io.Reader
	switch compression {
	case compressionGzip:
		return nil, fmt.Errorf("gzip compression not commonly used in regions")
	case compressionZlib:
		zr, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("creating zlib reader: %w", err)
		}
		defer zr.Close()
		reader = zr
	case compressionNone:
		reader = bytes.NewReader(data)
	default:
		return nil, fmt.Errorf("unsupported compression type: %d", compression)
	}

	// Read decompressed NBT
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("decompressing chunk: %w", err)
	}

	return nbt.ReadCompound(decompressed)
}

// WriteChunk writes a chunk's NBT data to the region
func (r *Region) WriteChunk(localX, localZ int, chunk nbt.Compound) error {
	if localX < 0 || localX >= 32 || localZ < 0 || localZ >= 32 {
		return ErrInvalidPosition
	}

	// Serialize NBT to bytes
	nbtData, err := nbt.WriteCompound("", chunk, nbt.CompressionNone)
	if err != nil {
		return fmt.Errorf("serializing chunk NBT: %w", err)
	}

	// Compress with zlib
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(nbtData); err != nil {
		zw.Close()
		return fmt.Errorf("compressing chunk: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("closing zlib writer: %w", err)
	}

	compressedData := compressed.Bytes()
	totalLength := len(compressedData) + 5 // +4 for length, +1 for compression type

	if totalLength > maxChunkSize {
		return ErrChunkTooLarge
	}

	// Calculate sectors needed
	sectorsNeeded := (totalLength + sectorSize - 1) / sectorSize

	r.mu.Lock()
	defer r.mu.Unlock()

	idx := localX + localZ*32
	oldLocation := r.locations[idx]

	// Find space for chunk
	offset, err := r.findSpace(sectorsNeeded, oldLocation)
	if err != nil {
		return fmt.Errorf("finding space for chunk: %w", err)
	}

	// Write chunk data
	chunkBuf := make([]byte, sectorsNeeded*sectorSize)
	binary.BigEndian.PutUint32(chunkBuf[0:4], uint32(len(compressedData)+1))
	chunkBuf[4] = compressionZlib
	copy(chunkBuf[5:], compressedData)

	if _, err := r.file.WriteAt(chunkBuf, int64(offset)*sectorSize); err != nil {
		return fmt.Errorf("writing chunk data: %w", err)
	}

	// Update location table
	r.locations[idx] = (uint32(offset) << 8) | uint32(sectorsNeeded)
	r.dirty = true

	return r.writeHeader()
}

func (r *Region) findSpace(sectorsNeeded int, oldLocation uint32) (int, error) {
	// If old chunk exists and has enough space, reuse it
	if oldLocation != 0 {
		oldOffset := int((oldLocation >> 8) & 0xFFFFFF)
		oldSectors := int(oldLocation & 0xFF)
		if oldSectors >= sectorsNeeded {
			return oldOffset, nil
		}
	}

	// Find the end of used space
	maxOffset := 2 // Start after header (2 sectors)
	for _, loc := range r.locations {
		if loc == 0 {
			continue
		}
		offset := int((loc >> 8) & 0xFFFFFF)
		sectors := int(loc & 0xFF)
		end := offset + sectors
		if end > maxOffset {
			maxOffset = end
		}
	}

	return maxOffset, nil
}

func (r *Region) writeHeader() error {
	buf := make([]byte, headerSize)

	// Write locations
	for i, loc := range r.locations {
		offset := i * 4
		binary.BigEndian.PutUint32(buf[offset:offset+4], loc)
	}

	// Write timestamps
	for i, ts := range r.timestamps {
		offset := 4096 + i*4
		binary.BigEndian.PutUint32(buf[offset:offset+4], ts)
	}

	if _, err := r.file.WriteAt(buf, 0); err != nil {
		return fmt.Errorf("writing region header: %w", err)
	}

	r.dirty = false
	return nil
}

// Close closes the region file
func (r *Region) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.dirty {
		if err := r.writeHeader(); err != nil {
			r.file.Close()
			return err
		}
	}

	return r.file.Close()
}

// ListChunks returns coordinates of all chunks present in the region
func (r *Region) ListChunks() [][2]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var chunks [][2]int
	for i, loc := range r.locations {
		if loc != 0 {
			localX := i % 32
			localZ := i / 32
			chunks = append(chunks, [2]int{localX, localZ})
		}
	}
	return chunks
}

// RegionManager manages access to region files
type RegionManager struct {
	worldPath string
	dimension string
	regions   map[string]*Region
	mu        sync.RWMutex
}

// NewRegionManager creates a new region manager
func NewRegionManager(worldPath, dimension string) *RegionManager {
	return &RegionManager{
		worldPath: worldPath,
		dimension: dimension,
		regions:   make(map[string]*Region),
	}
}

// GetRegion gets or opens a region file
func (m *RegionManager) GetRegion(rx, rz int) (*Region, error) {
	key := fmt.Sprintf("%d,%d", rx, rz)

	m.mu.RLock()
	if r, ok := m.regions[key]; ok {
		m.mu.RUnlock()
		return r, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double check
	if r, ok := m.regions[key]; ok {
		return r, nil
	}

	path := RegionPath(m.worldPath, m.dimension, rx, rz)

	// Try to open existing region
	if _, err := os.Stat(path); err == nil {
		r, err := OpenRegion(path)
		if err != nil {
			return nil, err
		}
		m.regions[key] = r
		return r, nil
	}

	// Create new region
	r, err := CreateRegion(path)
	if err != nil {
		return nil, err
	}
	m.regions[key] = r
	return r, nil
}

// GetChunk reads a chunk from the appropriate region
func (m *RegionManager) GetChunk(chunkX, chunkZ int) (nbt.Compound, error) {
	rx, rz := RegionCoord(chunkX, chunkZ)
	region, err := m.GetRegion(rx, rz)
	if err != nil {
		return nil, err
	}

	localX := chunkX & 31
	localZ := chunkZ & 31
	if chunkX < 0 {
		localX = ((chunkX % 32) + 32) % 32
	}
	if chunkZ < 0 {
		localZ = ((chunkZ % 32) + 32) % 32
	}

	return region.ReadChunk(localX, localZ)
}

// WriteChunk writes a chunk to the appropriate region
func (m *RegionManager) WriteChunk(chunkX, chunkZ int, chunk nbt.Compound) error {
	rx, rz := RegionCoord(chunkX, chunkZ)
	region, err := m.GetRegion(rx, rz)
	if err != nil {
		return err
	}

	localX := chunkX & 31
	localZ := chunkZ & 31
	if chunkX < 0 {
		localX = ((chunkX % 32) + 32) % 32
	}
	if chunkZ < 0 {
		localZ = ((chunkZ % 32) + 32) % 32
	}

	return region.WriteChunk(localX, localZ, chunk)
}

// Close closes all open regions
func (m *RegionManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for _, r := range m.regions {
		if err := r.Close(); err != nil {
			lastErr = err
		}
	}
	m.regions = make(map[string]*Region)
	return lastErr
}
