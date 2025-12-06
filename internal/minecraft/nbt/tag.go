package nbt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// Tag type IDs as defined in NBT specification
const (
	TagEnd       byte = 0
	TagByte      byte = 1
	TagShort     byte = 2
	TagInt       byte = 3
	TagLong      byte = 4
	TagFloat     byte = 5
	TagDouble    byte = 6
	TagByteArray byte = 7
	TagString    byte = 8
	TagList      byte = 9
	TagCompound  byte = 10
	TagIntArray  byte = 11
	TagLongArray byte = 12
)

var (
	ErrInvalidTag    = errors.New("invalid NBT tag")
	ErrUnexpectedEOF = errors.New("unexpected end of NBT data")
	ErrInvalidName   = errors.New("invalid tag name")
)

// Tag represents an NBT tag with name and value
type Tag struct {
	Type  byte
	Name  string
	Value interface{}
}

// Compound is a map of named tags
type Compound map[string]*Tag

// List is a slice of tag values (all same type)
type List struct {
	Type   byte
	Values []interface{}
}

// Reader provides NBT parsing functionality
type Reader struct {
	r   io.Reader
	buf []byte
}

// NewReader creates a new NBT reader
func NewReader(r io.Reader) *Reader {
	return &Reader{
		r:   r,
		buf: make([]byte, 8),
	}
}

// ReadTag reads a complete named tag
func (r *Reader) ReadTag() (*Tag, error) {
	tagType, err := r.readByte()
	if err != nil {
		return nil, err
	}

	if tagType == TagEnd {
		return &Tag{Type: TagEnd}, nil
	}

	name, err := r.readString()
	if err != nil {
		return nil, fmt.Errorf("reading tag name: %w", err)
	}

	value, err := r.readPayload(tagType)
	if err != nil {
		return nil, fmt.Errorf("reading tag payload for %q: %w", name, err)
	}

	return &Tag{
		Type:  tagType,
		Name:  name,
		Value: value,
	}, nil
}

// ReadCompound reads a compound tag value (without type/name prefix)
func (r *Reader) ReadCompound() (Compound, error) {
	compound := make(Compound)

	for {
		tag, err := r.ReadTag()
		if err != nil {
			return nil, err
		}

		if tag.Type == TagEnd {
			break
		}

		compound[tag.Name] = tag
	}

	return compound, nil
}

func (r *Reader) readPayload(tagType byte) (interface{}, error) {
	switch tagType {
	case TagByte:
		return r.readByte()
	case TagShort:
		return r.readShort()
	case TagInt:
		return r.readInt()
	case TagLong:
		return r.readLong()
	case TagFloat:
		return r.readFloat()
	case TagDouble:
		return r.readDouble()
	case TagByteArray:
		return r.readByteArray()
	case TagString:
		return r.readString()
	case TagList:
		return r.readList()
	case TagCompound:
		return r.ReadCompound()
	case TagIntArray:
		return r.readIntArray()
	case TagLongArray:
		return r.readLongArray()
	default:
		return nil, fmt.Errorf("%w: unknown type %d", ErrInvalidTag, tagType)
	}
}

func (r *Reader) readByte() (byte, error) {
	if _, err := io.ReadFull(r.r, r.buf[:1]); err != nil {
		return 0, err
	}
	return r.buf[0], nil
}

func (r *Reader) readShort() (int16, error) {
	if _, err := io.ReadFull(r.r, r.buf[:2]); err != nil {
		return 0, err
	}
	return int16(binary.BigEndian.Uint16(r.buf[:2])), nil
}

func (r *Reader) readInt() (int32, error) {
	if _, err := io.ReadFull(r.r, r.buf[:4]); err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(r.buf[:4])), nil
}

func (r *Reader) readLong() (int64, error) {
	if _, err := io.ReadFull(r.r, r.buf[:8]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(r.buf[:8])), nil
}

func (r *Reader) readFloat() (float32, error) {
	if _, err := io.ReadFull(r.r, r.buf[:4]); err != nil {
		return 0, err
	}
	return math.Float32frombits(binary.BigEndian.Uint32(r.buf[:4])), nil
}

func (r *Reader) readDouble() (float64, error) {
	if _, err := io.ReadFull(r.r, r.buf[:8]); err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.BigEndian.Uint64(r.buf[:8])), nil
}

func (r *Reader) readString() (string, error) {
	length, err := r.readShort()
	if err != nil {
		return "", err
	}

	if length < 0 {
		return "", fmt.Errorf("%w: negative string length", ErrInvalidTag)
	}

	if length == 0 {
		return "", nil
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r.r, data); err != nil {
		return "", err
	}

	return string(data), nil
}

func (r *Reader) readByteArray() ([]byte, error) {
	length, err := r.readInt()
	if err != nil {
		return nil, err
	}

	if length < 0 {
		return nil, fmt.Errorf("%w: negative array length", ErrInvalidTag)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r.r, data); err != nil {
		return nil, err
	}

	return data, nil
}

func (r *Reader) readIntArray() ([]int32, error) {
	length, err := r.readInt()
	if err != nil {
		return nil, err
	}

	if length < 0 {
		return nil, fmt.Errorf("%w: negative array length", ErrInvalidTag)
	}

	data := make([]int32, length)
	for i := int32(0); i < length; i++ {
		data[i], err = r.readInt()
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}

func (r *Reader) readLongArray() ([]int64, error) {
	length, err := r.readInt()
	if err != nil {
		return nil, err
	}

	if length < 0 {
		return nil, fmt.Errorf("%w: negative array length", ErrInvalidTag)
	}

	data := make([]int64, length)
	for i := int32(0); i < length; i++ {
		data[i], err = r.readLong()
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}

func (r *Reader) readList() (*List, error) {
	elemType, err := r.readByte()
	if err != nil {
		return nil, err
	}

	length, err := r.readInt()
	if err != nil {
		return nil, err
	}

	if length < 0 {
		return nil, fmt.Errorf("%w: negative list length", ErrInvalidTag)
	}

	list := &List{
		Type:   elemType,
		Values: make([]interface{}, length),
	}

	for i := int32(0); i < length; i++ {
		list.Values[i], err = r.readPayload(elemType)
		if err != nil {
			return nil, fmt.Errorf("reading list element %d: %w", i, err)
		}
	}

	return list, nil
}

// Writer provides NBT writing functionality
type Writer struct {
	w   io.Writer
	buf []byte
}

// NewWriter creates a new NBT writer
func NewWriter(w io.Writer) *Writer {
	return &Writer{
		w:   w,
		buf: make([]byte, 8),
	}
}

// WriteTag writes a complete named tag
func (w *Writer) WriteTag(tag *Tag) error {
	if err := w.writeByte(tag.Type); err != nil {
		return err
	}

	if tag.Type == TagEnd {
		return nil
	}

	if err := w.writeString(tag.Name); err != nil {
		return err
	}

	return w.writePayload(tag.Type, tag.Value)
}

// WriteCompound writes a compound value
func (w *Writer) WriteCompound(compound Compound) error {
	for name, tag := range compound {
		tag.Name = name
		if err := w.WriteTag(tag); err != nil {
			return err
		}
	}
	return w.writeByte(TagEnd)
}

func (w *Writer) writePayload(tagType byte, value interface{}) error {
	switch tagType {
	case TagByte:
		return w.writeByte(value.(byte))
	case TagShort:
		return w.writeShort(value.(int16))
	case TagInt:
		return w.writeInt(value.(int32))
	case TagLong:
		return w.writeLong(value.(int64))
	case TagFloat:
		return w.writeFloat(value.(float32))
	case TagDouble:
		return w.writeDouble(value.(float64))
	case TagByteArray:
		return w.writeByteArray(value.([]byte))
	case TagString:
		return w.writeString(value.(string))
	case TagList:
		return w.writeList(value.(*List))
	case TagCompound:
		return w.WriteCompound(value.(Compound))
	case TagIntArray:
		return w.writeIntArray(value.([]int32))
	case TagLongArray:
		return w.writeLongArray(value.([]int64))
	default:
		return fmt.Errorf("%w: unknown type %d", ErrInvalidTag, tagType)
	}
}

func (w *Writer) writeByte(v byte) error {
	w.buf[0] = v
	_, err := w.w.Write(w.buf[:1])
	return err
}

func (w *Writer) writeShort(v int16) error {
	binary.BigEndian.PutUint16(w.buf[:2], uint16(v))
	_, err := w.w.Write(w.buf[:2])
	return err
}

func (w *Writer) writeInt(v int32) error {
	binary.BigEndian.PutUint32(w.buf[:4], uint32(v))
	_, err := w.w.Write(w.buf[:4])
	return err
}

func (w *Writer) writeLong(v int64) error {
	binary.BigEndian.PutUint64(w.buf[:8], uint64(v))
	_, err := w.w.Write(w.buf[:8])
	return err
}

func (w *Writer) writeFloat(v float32) error {
	binary.BigEndian.PutUint32(w.buf[:4], math.Float32bits(v))
	_, err := w.w.Write(w.buf[:4])
	return err
}

func (w *Writer) writeDouble(v float64) error {
	binary.BigEndian.PutUint64(w.buf[:8], math.Float64bits(v))
	_, err := w.w.Write(w.buf[:8])
	return err
}

func (w *Writer) writeString(v string) error {
	if len(v) > 32767 {
		return fmt.Errorf("%w: string too long", ErrInvalidTag)
	}
	if err := w.writeShort(int16(len(v))); err != nil {
		return err
	}
	_, err := w.w.Write([]byte(v))
	return err
}

func (w *Writer) writeByteArray(v []byte) error {
	if err := w.writeInt(int32(len(v))); err != nil {
		return err
	}
	_, err := w.w.Write(v)
	return err
}

func (w *Writer) writeIntArray(v []int32) error {
	if err := w.writeInt(int32(len(v))); err != nil {
		return err
	}
	for _, val := range v {
		if err := w.writeInt(val); err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) writeLongArray(v []int64) error {
	if err := w.writeInt(int32(len(v))); err != nil {
		return err
	}
	for _, val := range v {
		if err := w.writeLong(val); err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) writeList(list *List) error {
	if err := w.writeByte(list.Type); err != nil {
		return err
	}
	if err := w.writeInt(int32(len(list.Values))); err != nil {
		return err
	}
	for _, val := range list.Values {
		if err := w.writePayload(list.Type, val); err != nil {
			return err
		}
	}
	return nil
}

// Helper methods for Compound

// GetByte returns byte value from compound
func (c Compound) GetByte(name string) (byte, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagByte {
		return 0, false
	}
	return tag.Value.(byte), true
}

// GetShort returns int16 value from compound
func (c Compound) GetShort(name string) (int16, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagShort {
		return 0, false
	}
	return tag.Value.(int16), true
}

// GetInt returns int32 value from compound
func (c Compound) GetInt(name string) (int32, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagInt {
		return 0, false
	}
	return tag.Value.(int32), true
}

// GetLong returns int64 value from compound
func (c Compound) GetLong(name string) (int64, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagLong {
		return 0, false
	}
	return tag.Value.(int64), true
}

// GetFloat returns float32 value from compound
func (c Compound) GetFloat(name string) (float32, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagFloat {
		return 0, false
	}
	return tag.Value.(float32), true
}

// GetDouble returns float64 value from compound
func (c Compound) GetDouble(name string) (float64, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagDouble {
		return 0, false
	}
	return tag.Value.(float64), true
}

// GetString returns string value from compound
func (c Compound) GetString(name string) (string, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagString {
		return "", false
	}
	return tag.Value.(string), true
}

// GetByteArray returns []byte from compound
func (c Compound) GetByteArray(name string) ([]byte, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagByteArray {
		return nil, false
	}
	return tag.Value.([]byte), true
}

// GetIntArray returns []int32 from compound
func (c Compound) GetIntArray(name string) ([]int32, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagIntArray {
		return nil, false
	}
	return tag.Value.([]int32), true
}

// GetLongArray returns []int64 from compound
func (c Compound) GetLongArray(name string) ([]int64, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagLongArray {
		return nil, false
	}
	return tag.Value.([]int64), true
}

// GetList returns *List from compound
func (c Compound) GetList(name string) (*List, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagList {
		return nil, false
	}
	return tag.Value.(*List), true
}

// GetCompound returns Compound from compound
func (c Compound) GetCompound(name string) (Compound, bool) {
	tag, ok := c[name]
	if !ok || tag.Type != TagCompound {
		return nil, false
	}
	return tag.Value.(Compound), true
}

// Set sets a value in the compound
func (c Compound) Set(name string, tagType byte, value interface{}) {
	c[name] = &Tag{Type: tagType, Name: name, Value: value}
}

// SetByte sets a byte value
func (c Compound) SetByte(name string, value byte) {
	c.Set(name, TagByte, value)
}

// SetShort sets an int16 value
func (c Compound) SetShort(name string, value int16) {
	c.Set(name, TagShort, value)
}

// SetInt sets an int32 value
func (c Compound) SetInt(name string, value int32) {
	c.Set(name, TagInt, value)
}

// SetLong sets an int64 value
func (c Compound) SetLong(name string, value int64) {
	c.Set(name, TagLong, value)
}

// SetFloat sets a float32 value
func (c Compound) SetFloat(name string, value float32) {
	c.Set(name, TagFloat, value)
}

// SetDouble sets a float64 value
func (c Compound) SetDouble(name string, value float64) {
	c.Set(name, TagDouble, value)
}

// SetString sets a string value
func (c Compound) SetString(name string, value string) {
	c.Set(name, TagString, value)
}

// SetByteArray sets a []byte value
func (c Compound) SetByteArray(name string, value []byte) {
	c.Set(name, TagByteArray, value)
}

// SetIntArray sets a []int32 value
func (c Compound) SetIntArray(name string, value []int32) {
	c.Set(name, TagIntArray, value)
}

// SetLongArray sets a []int64 value
func (c Compound) SetLongArray(name string, value []int64) {
	c.Set(name, TagLongArray, value)
}

// SetList sets a *List value
func (c Compound) SetList(name string, value *List) {
	c.Set(name, TagList, value)
}

// SetCompound sets a Compound value
func (c Compound) SetCompound(name string, value Compound) {
	c.Set(name, TagCompound, value)
}
