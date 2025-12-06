package nbt

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
)

// Compression types used in Minecraft
const (
	CompressionGzip       = 1
	CompressionZlib       = 2
	CompressionNone       = 3
	CompressionLZ4        = 4
	CompressionCustom     = 127
	CompressionAutoDetect = 255
)

// Read reads NBT data with automatic compression detection
func Read(data []byte) (*Tag, error) {
	return ReadWithCompression(data, CompressionAutoDetect)
}

// ReadWithCompression reads NBT data with specified compression
func ReadWithCompression(data []byte, compression byte) (*Tag, error) {
	if len(data) == 0 {
		return nil, ErrUnexpectedEOF
	}

	var reader io.Reader = bytes.NewReader(data)
	var err error

	if compression == CompressionAutoDetect {
		compression = detectCompression(data)
	}

	switch compression {
	case CompressionGzip:
		reader, err = gzip.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("creating gzip reader: %w", err)
		}
		defer reader.(*gzip.Reader).Close()
	case CompressionZlib:
		reader, err = zlib.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("creating zlib reader: %w", err)
		}
		defer reader.(io.ReadCloser).Close()
	case CompressionNone:
		// Use raw reader
	case CompressionLZ4:
		return nil, fmt.Errorf("LZ4 compression not yet supported")
	default:
		return nil, fmt.Errorf("unknown compression type: %d", compression)
	}

	return NewReader(reader).ReadTag()
}

// Write writes NBT data with specified compression
func Write(tag *Tag, compression byte) ([]byte, error) {
	var buf bytes.Buffer
	var writer io.Writer = &buf

	switch compression {
	case CompressionGzip:
		gw := gzip.NewWriter(&buf)
		writer = gw
		defer gw.Close()
	case CompressionZlib:
		zw := zlib.NewWriter(&buf)
		writer = zw
		defer zw.Close()
	case CompressionNone:
		// Use raw buffer
	default:
		return nil, fmt.Errorf("unsupported compression for writing: %d", compression)
	}

	if err := NewWriter(writer).WriteTag(tag); err != nil {
		return nil, err
	}

	// Close compression writers to flush
	switch compression {
	case CompressionGzip:
		if err := writer.(*gzip.Writer).Close(); err != nil {
			return nil, err
		}
	case CompressionZlib:
		if err := writer.(*zlib.Writer).Close(); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// detectCompression attempts to detect the compression type from data
func detectCompression(data []byte) byte {
	if len(data) < 2 {
		return CompressionNone
	}

	// Check for gzip magic number (0x1f 0x8b)
	if data[0] == 0x1f && data[1] == 0x8b {
		return CompressionGzip
	}

	// Check for zlib header
	// Zlib header is typically 0x78 followed by 0x01, 0x5e, 0x9c, or 0xda
	if data[0] == 0x78 {
		switch data[1] {
		case 0x01, 0x5e, 0x9c, 0xda:
			return CompressionZlib
		}
	}

	// Check if it starts with a valid NBT tag type (compound tag = 0x0a)
	if data[0] == TagCompound {
		return CompressionNone
	}

	// Default to zlib for region file chunks
	return CompressionZlib
}

// ReadCompound reads NBT data and returns the root compound
func ReadCompound(data []byte) (Compound, error) {
	tag, err := Read(data)
	if err != nil {
		return nil, err
	}

	if tag.Type != TagCompound {
		return nil, fmt.Errorf("expected root compound tag, got type %d", tag.Type)
	}

	compound, ok := tag.Value.(Compound)
	if !ok {
		return nil, fmt.Errorf("root tag value is not a compound")
	}

	return compound, nil
}

// WriteCompound writes a compound tag with the given name and compression
func WriteCompound(name string, compound Compound, compression byte) ([]byte, error) {
	tag := &Tag{
		Type:  TagCompound,
		Name:  name,
		Value: compound,
	}
	return Write(tag, compression)
}
