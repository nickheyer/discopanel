package proxy

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// maxHandshakeLength bounds the initial handshake packet. The vanilla
// handshake is tiny, but modded clients (Forge FML markers) append data to
// the address field, so allow generous headroom while still bounding memory.
const maxHandshakeLength = 2048

// legacyPingByte is the first byte of a pre-1.7 server list ping. Modern
// framing never starts a handshake with it (it would imply a 254-byte
// handshake, far larger than vanilla clients send).
const legacyPingByte = 0xFE

// VarInt reads/writes variable-length integers as used in Minecraft protocol
type VarInt int32

// ReadVarInt reads a variable-length integer from the reader
func ReadVarInt(r io.Reader) (VarInt, error) {
	var value int32
	var position int

	for {
		b, err := readByte(r)
		if err != nil {
			return 0, fmt.Errorf("failed to read varint byte: %w", err)
		}

		value |= int32(b&0x7F) << position

		if b&0x80 == 0 {
			break
		}

		position += 7

		if position >= 32 {
			return 0, fmt.Errorf("VarInt is too big")
		}
	}

	return VarInt(value), nil
}

// readByte reads a single byte, using the reader's ByteReader fast path when
// available (bufio.Reader, bytes.Reader) instead of a per-byte Read call.
func readByte(r io.Reader) (byte, error) {
	if br, ok := r.(io.ByteReader); ok {
		return br.ReadByte()
	}
	var buf [1]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return buf[0], nil
}

// WriteVarInt writes a variable-length integer to the writer
func WriteVarInt(w io.Writer, value VarInt) error {
	for {
		if (value & ^0x7F) == 0 {
			return binary.Write(w, binary.BigEndian, byte(value))
		}

		if err := binary.Write(w, binary.BigEndian, byte((value&0x7F)|0x80)); err != nil {
			return err
		}

		value >>= 7
	}
}

// Len returns the length of the VarInt when encoded
func (v VarInt) Len() int {
	value := int32(v)
	length := 0
	for {
		length++
		if (value & ^0x7F) == 0 {
			break
		}
		value >>= 7
	}
	return length
}

// Handshake next-state values.
const (
	NextStateStatus = 1
	NextStateLogin  = 2
)

// HandshakePacket represents the initial handshake packet from a Minecraft client
type HandshakePacket struct {
	ProtocolVersion VarInt
	ServerAddress   string
	ServerPort      uint16
	NextState       VarInt // 1 for status, 2 for login
}

// ReadHandshakePacket reads a handshake packet from the connection
func ReadHandshakePacket(r io.Reader) (*HandshakePacket, error) {
	length, err := ReadVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read packet length: %w", err)
	}

	if length < 1 || length > maxHandshakeLength {
		return nil, fmt.Errorf("invalid handshake length: %d", length)
	}

	data := make([]byte, length)
	n, err := io.ReadFull(r, data)
	if err != nil {
		return nil, fmt.Errorf("failed to read packet data (got %d/%d bytes): %w", n, length, err)
	}

	buf := bytes.NewReader(data)

	packetID, err := ReadVarInt(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read packet ID: %w", err)
	}

	if packetID != 0x00 {
		return nil, fmt.Errorf("expected handshake packet (0x00), got %d", packetID)
	}

	packet := &HandshakePacket{}

	packet.ProtocolVersion, err = ReadVarInt(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read protocol version: %w", err)
	}

	addressLen, err := ReadVarInt(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read address length: %w", err)
	}
	if addressLen < 0 || int(addressLen) > buf.Len() {
		return nil, fmt.Errorf("invalid address length: %d", addressLen)
	}

	addressBytes := make([]byte, addressLen)
	if _, err := io.ReadFull(buf, addressBytes); err != nil {
		return nil, fmt.Errorf("failed to read address: %w", err)
	}
	packet.ServerAddress = string(addressBytes)

	if err := binary.Read(buf, binary.BigEndian, &packet.ServerPort); err != nil {
		return nil, fmt.Errorf("failed to read port: %w", err)
	}

	packet.NextState, err = ReadVarInt(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read next state: %w", err)
	}

	return packet, nil
}

// WriteHandshakePacket writes a handshake packet to the connection
func WriteHandshakePacket(w io.Writer, packet *HandshakePacket) error {
	var buf bytes.Buffer

	if err := WriteVarInt(&buf, 0x00); err != nil {
		return err
	}
	if err := WriteVarInt(&buf, packet.ProtocolVersion); err != nil {
		return err
	}
	if err := WriteVarInt(&buf, VarInt(len(packet.ServerAddress))); err != nil {
		return err
	}
	if _, err := buf.WriteString(packet.ServerAddress); err != nil {
		return err
	}
	if err := binary.Write(&buf, binary.BigEndian, packet.ServerPort); err != nil {
		return err
	}
	if err := WriteVarInt(&buf, packet.NextState); err != nil {
		return err
	}

	return writeFramed(w, buf.Bytes())
}

// writeFramed writes a length-prefixed Minecraft packet.
func writeFramed(w io.Writer, data []byte) error {
	var buf bytes.Buffer
	if err := WriteVarInt(&buf, VarInt(len(data))); err != nil {
		return err
	}
	buf.Write(data)
	_, err := w.Write(buf.Bytes())
	return err
}

// WriteLoginDisconnect sends a login-state disconnect packet (0x00) carrying
// a chat-component reason, so clients see a message instead of a bare
// connection reset.
func WriteLoginDisconnect(w io.Writer, message string) error {
	reason, err := json.Marshal(map[string]string{"text": message})
	if err != nil {
		return err
	}
	var payload bytes.Buffer
	if err := WriteVarInt(&payload, 0x00); err != nil {
		return err
	}
	if err := WriteVarInt(&payload, VarInt(len(reason))); err != nil {
		return err
	}
	payload.Write(reason)
	return writeFramed(w, payload.Bytes())
}
