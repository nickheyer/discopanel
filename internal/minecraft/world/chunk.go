package world

import (
	"fmt"
	"strings"

	"github.com/nickheyer/discopanel/internal/minecraft/nbt"
)

const (
	sectionHeight    = 16
	sectionVolume    = 16 * 16 * 16
	biomeVolume      = 4 * 4 * 4
	minBitsPerBlock  = 4
	directPaletteBit = 15
)

// BlockState represents a Minecraft block with properties
type BlockState struct {
	Name       string
	Properties map[string]string
}

// Section represents a 16x16x16 section of blocks
type Section struct {
	Y           int
	Palette     []BlockState
	BlockStates []int // Indices into palette
	BlockLight  []byte
	SkyLight    []byte
	// Preserve original biomes data
	Biomes *nbt.Compound
}

// Chunk represents parsed chunk data
type Chunk struct {
	X           int
	Z           int
	DataVersion int
	Sections    map[int]*Section
	Heightmap   []int64
	MinY        int
	MaxY        int
	// Preserve original NBT for fields we don't modify
	OriginalNBT nbt.Compound
}

// GetBlock returns the block at the given position within the chunk
func (c *Chunk) GetBlock(x, y, z int) (*BlockState, error) {
	if x < 0 || x >= 16 || z < 0 || z >= 16 {
		return nil, fmt.Errorf("x/z out of bounds: %d, %d", x, z)
	}

	sectionY := y >> 4
	section, ok := c.Sections[sectionY]
	if !ok {
		return &BlockState{Name: "minecraft:air"}, nil
	}

	localY := y & 15
	idx := localY*256 + z*16 + x

	if idx >= len(section.BlockStates) {
		return &BlockState{Name: "minecraft:air"}, nil
	}

	paletteIdx := section.BlockStates[idx]
	if paletteIdx >= len(section.Palette) {
		return &BlockState{Name: "minecraft:air"}, nil
	}

	block := section.Palette[paletteIdx]
	return &block, nil
}

// SetBlock sets the block at the given position within the chunk
func (c *Chunk) SetBlock(x, y, z int, block BlockState) error {
	if x < 0 || x >= 16 || z < 0 || z >= 16 {
		return fmt.Errorf("x/z out of bounds: %d, %d", x, z)
	}

	sectionY := y >> 4
	section, ok := c.Sections[sectionY]
	if !ok {
		// Create new section
		section = &Section{
			Y:           sectionY,
			Palette:     []BlockState{{Name: "minecraft:air"}},
			BlockStates: make([]int, sectionVolume),
		}
		c.Sections[sectionY] = section
	}

	// Find or add block to palette
	paletteIdx := -1
	for i, p := range section.Palette {
		if p.Name == block.Name && mapsEqual(p.Properties, block.Properties) {
			paletteIdx = i
			break
		}
	}

	if paletteIdx == -1 {
		paletteIdx = len(section.Palette)
		section.Palette = append(section.Palette, block)
	}

	localY := y & 15
	idx := localY*256 + z*16 + x
	section.BlockStates[idx] = paletteIdx

	return nil
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// ParseChunk parses NBT chunk data into a Chunk struct
func ParseChunk(data nbt.Compound) (*Chunk, error) {
	chunk := &Chunk{
		Sections:    make(map[int]*Section),
		MinY:        -64,
		MaxY:        320,
		OriginalNBT: data, // Preserve original NBT
	}

	// Get chunk coordinates
	if x, ok := data.GetInt("xPos"); ok {
		chunk.X = int(x)
	}
	if z, ok := data.GetInt("zPos"); ok {
		chunk.Z = int(z)
	}

	// Get data version
	if dv, ok := data.GetInt("DataVersion"); ok {
		chunk.DataVersion = int(dv)
	}

	// Get yPos for min Y (1.18+)
	if yPos, ok := data.GetInt("yPos"); ok {
		chunk.MinY = int(yPos) * 16
	}

	// Parse heightmaps
	if heightmaps, ok := data.GetCompound("Heightmaps"); ok {
		if motionBlocking, ok := heightmaps.GetLongArray("MOTION_BLOCKING"); ok {
			chunk.Heightmap = motionBlocking
		}
	}

	// Parse sections - modern format (1.18+)
	if sections, ok := data.GetList("sections"); ok {
		for _, sectionVal := range sections.Values {
			sectionData, ok := sectionVal.(nbt.Compound)
			if !ok {
				continue
			}

			section, err := parseSection(sectionData)
			if err != nil {
				continue
			}

			chunk.Sections[section.Y] = section
		}
	}

	return chunk, nil
}

func parseSection(data nbt.Compound) (*Section, error) {
	section := &Section{}

	// Get Y position
	if y, ok := data.GetByte("Y"); ok {
		section.Y = int(int8(y))
	}

	// Parse block_states
	if blockStates, ok := data.GetCompound("block_states"); ok {
		section.Palette, section.BlockStates = parseBlockStates(blockStates)
	}

	// Parse biomes (preserve them)
	if biomes, ok := data.GetCompound("biomes"); ok {
		section.Biomes = &biomes
	}

	// Parse light data
	if blockLight, ok := data.GetByteArray("BlockLight"); ok {
		section.BlockLight = blockLight
	}
	if skyLight, ok := data.GetByteArray("SkyLight"); ok {
		section.SkyLight = skyLight
	}

	return section, nil
}

func parseBlockStates(data nbt.Compound) ([]BlockState, []int) {
	// Parse palette
	var palette []BlockState
	if paletteList, ok := data.GetList("palette"); ok {
		for _, entry := range paletteList.Values {
			entryData, ok := entry.(nbt.Compound)
			if !ok {
				continue
			}

			block := BlockState{
				Properties: make(map[string]string),
			}

			if name, ok := entryData.GetString("Name"); ok {
				block.Name = name
			}

			if props, ok := entryData.GetCompound("Properties"); ok {
				for k, v := range props {
					if v.Type == nbt.TagString {
						block.Properties[k] = v.Value.(string)
					}
				}
			}

			palette = append(palette, block)
		}
	}

	// If palette has only one entry, entire section is that block
	if len(palette) <= 1 {
		blocks := make([]int, sectionVolume)
		// All zeros, pointing to first (and only) palette entry
		return palette, blocks
	}

	// Parse packed block data
	blocks := make([]int, sectionVolume)

	if packedData, ok := data.GetLongArray("data"); ok {
		bitsPerBlock := calculateBitsPerBlock(len(palette))
		unpackBlockStates(packedData, blocks, bitsPerBlock)
	}

	return palette, blocks
}

func calculateBitsPerBlock(paletteSize int) int {
	if paletteSize <= 1 {
		return 0
	}

	bits := 0
	for (1 << bits) < paletteSize {
		bits++
	}

	if bits < minBitsPerBlock {
		bits = minBitsPerBlock
	}

	return bits
}

func unpackBlockStates(packed []int64, unpacked []int, bitsPerBlock int) {
	if bitsPerBlock == 0 || len(packed) == 0 {
		return
	}

	blocksPerLong := 64 / bitsPerBlock
	mask := int64((1 << bitsPerBlock) - 1)

	blockIdx := 0
	for _, long := range packed {
		for i := 0; i < blocksPerLong && blockIdx < len(unpacked); i++ {
			unpacked[blockIdx] = int(long & mask)
			long >>= bitsPerBlock
			blockIdx++
		}
	}
}

// ToNBT converts a Chunk back to NBT format for saving
// This preserves the original NBT structure and only updates the sections we modified
func (c *Chunk) ToNBT() nbt.Compound {
	// Start with a copy of the original NBT to preserve all fields
	root := make(nbt.Compound)
	for k, v := range c.OriginalNBT {
		root[k] = v
	}

	// Ensure critical coordinates are set correctly
	root.SetInt("xPos", int32(c.X))
	root.SetInt("zPos", int32(c.Z))
	root.SetInt("DataVersion", int32(c.DataVersion))
	root.SetInt("yPos", int32(c.MinY/16))

	// Ensure status is set
	if _, ok := root.GetString("Status"); !ok {
		root.SetString("Status", "minecraft:full")
	}

	// Build the sections list, preserving biomes and other data
	sectionList := &nbt.List{
		Type:   nbt.TagCompound,
		Values: make([]interface{}, 0),
	}

	// Get existing sections from original NBT to merge
	existingSections := make(map[int]nbt.Compound)
	if origSections, ok := c.OriginalNBT.GetList("sections"); ok {
		for _, sv := range origSections.Values {
			if sc, ok := sv.(nbt.Compound); ok {
				if y, ok := sc.GetByte("Y"); ok {
					existingSections[int(int8(y))] = sc
				}
			}
		}
	}

	// Merge our modified sections with original data
	for sectionY, section := range c.Sections {
		sectionData := sectionToNBT(section)

		// If there was an existing section, preserve biomes and other data
		if origSection, ok := existingSections[sectionY]; ok {
			// Preserve biomes
			if biomes, ok := origSection.GetCompound("biomes"); ok {
				sectionData.SetCompound("biomes", biomes)
			}
		}

		sectionList.Values = append(sectionList.Values, sectionData)
	}

	// Add sections that we didn't modify (but exist in original)
	for y, origSection := range existingSections {
		if _, modified := c.Sections[y]; !modified {
			sectionList.Values = append(sectionList.Values, origSection)
		}
	}

	root.SetList("sections", sectionList)

	// Update heightmaps if we have them
	if len(c.Heightmap) > 0 {
		if heightmaps, ok := root.GetCompound("Heightmaps"); ok {
			heightmaps.SetLongArray("MOTION_BLOCKING", c.Heightmap)
			root.SetCompound("Heightmaps", heightmaps)
		}
	}

	return root
}

func sectionToNBT(section *Section) nbt.Compound {
	data := make(nbt.Compound)
	data.SetByte("Y", byte(int8(section.Y)))

	// Block states
	blockStates := make(nbt.Compound)

	// Palette
	paletteList := &nbt.List{
		Type:   nbt.TagCompound,
		Values: make([]interface{}, 0, len(section.Palette)),
	}

	for _, block := range section.Palette {
		entry := make(nbt.Compound)
		entry.SetString("Name", block.Name)

		if len(block.Properties) > 0 {
			props := make(nbt.Compound)
			for k, v := range block.Properties {
				props.SetString(k, v)
			}
			entry.SetCompound("Properties", props)
		}

		paletteList.Values = append(paletteList.Values, entry)
	}

	blockStates.SetList("palette", paletteList)

	// Pack block states if palette has more than one entry
	if len(section.Palette) > 1 {
		bitsPerBlock := calculateBitsPerBlock(len(section.Palette))
		packed := packBlockStates(section.BlockStates, bitsPerBlock)
		blockStates.SetLongArray("data", packed)
	}

	data.SetCompound("block_states", blockStates)

	// Biomes - preserve from original or create default
	if section.Biomes != nil {
		data.SetCompound("biomes", *section.Biomes)
	} else {
		// Create default biome data (single-biome section)
		biomes := make(nbt.Compound)
		biomesPalette := &nbt.List{
			Type:   nbt.TagString,
			Values: []interface{}{"minecraft:plains"},
		}
		biomes.SetList("palette", biomesPalette)
		data.SetCompound("biomes", biomes)
	}

	// Light data
	if len(section.BlockLight) > 0 {
		data.SetByteArray("BlockLight", section.BlockLight)
	}
	if len(section.SkyLight) > 0 {
		data.SetByteArray("SkyLight", section.SkyLight)
	}

	return data
}

func packBlockStates(unpacked []int, bitsPerBlock int) []int64 {
	if bitsPerBlock == 0 {
		return nil
	}

	blocksPerLong := 64 / bitsPerBlock
	numLongs := (len(unpacked) + blocksPerLong - 1) / blocksPerLong
	packed := make([]int64, numLongs)

	blockIdx := 0
	for longIdx := range packed {
		var long int64
		for i := 0; i < blocksPerLong && blockIdx < len(unpacked); i++ {
			long |= int64(unpacked[blockIdx]) << (i * bitsPerBlock)
			blockIdx++
		}
		packed[longIdx] = long
	}

	return packed
}

// ParseBlockName parses a block name like "minecraft:stone" or "stone"
func ParseBlockName(name string) string {
	if !strings.Contains(name, ":") {
		return "minecraft:" + name
	}
	return name
}
