// discopanel-exporter: Prometheus exporter for Minecraft servers. Pings the
// configured servers with a Server List Ping on every scrape and exposes
// mc_status_* metrics (compatible with common Minecraft dashboards).
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	servers := splitServers(getEnv("EXPORT_SERVERS", "localhost:25565"))
	port := getEnv("EXPORT_PORT", "9225")
	timeout := 5 * time.Second

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		for _, addr := range servers {
			writeServerMetrics(w, addr, timeout)
		}
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok\n")
	})

	fmt.Printf("discopanel-exporter listening on :%s (servers: %s)\n", port, strings.Join(servers, ", "))
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
}

func writeServerMetrics(w io.Writer, addr string, timeout time.Duration) {
	labels := fmt.Sprintf(`server_host=%q,server_port=%q`, hostOf(addr), portOf(addr))

	status, latency, err := pingSLP(addr, timeout)
	if err != nil {
		fmt.Fprintf(w, "mc_status_healthy{%s} 0\n", labels)
		return
	}

	versionLabels := fmt.Sprintf(`%s,server_version=%q`, labels, status.Version.Name)
	fmt.Fprintf(w, "mc_status_healthy{%s} 1\n", versionLabels)
	fmt.Fprintf(w, "mc_status_response_time_seconds{%s} %f\n", versionLabels, latency.Seconds())
	fmt.Fprintf(w, "mc_status_players_online_count{%s} %d\n", versionLabels, status.Players.Online)
	fmt.Fprintf(w, "mc_status_players_max_count{%s} %d\n", versionLabels, status.Players.Max)
}

func splitServers(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			if !strings.Contains(part, ":") {
				part += ":25565"
			}
			out = append(out, part)
		}
	}
	return out
}

func hostOf(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

func portOf(addr string) string {
	if _, p, err := net.SplitHostPort(addr); err == nil {
		return p
	}
	return "25565"
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// slpStatus is the subset of the status response the exporter reports.
type slpStatus struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol"`
	} `json:"version"`
	Players struct {
		Max    int `json:"max"`
		Online int `json:"online"`
	} `json:"players"`
}

// pingSLP performs a minimal Server List Ping (handshake -> status request).
func pingSLP(addr string, timeout time.Duration) (*slpStatus, time.Duration, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	host := hostOf(addr)
	var port uint16 = 25565
	fmt.Sscanf(portOf(addr), "%d", &port)

	// Handshake (protocol -1 is accepted for status pings).
	var hs bytes.Buffer
	writeVarInt(&hs, 0x00)
	writeVarInt(&hs, -1)
	writeVarInt(&hs, int32(len(host)))
	hs.WriteString(host)
	binary.Write(&hs, binary.BigEndian, port)
	writeVarInt(&hs, 1)
	if err := writeFrame(conn, hs.Bytes()); err != nil {
		return nil, 0, err
	}

	// Status request.
	var req bytes.Buffer
	writeVarInt(&req, 0x00)
	if err := writeFrame(conn, req.Bytes()); err != nil {
		return nil, 0, err
	}

	// Status response.
	length, err := readVarInt(conn)
	if err != nil {
		return nil, 0, err
	}
	if length < 1 || length > 1024*1024 {
		return nil, 0, fmt.Errorf("invalid packet length %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, 0, err
	}
	latency := time.Since(start)

	reader := bytes.NewReader(data)
	packetID, err := readVarInt(reader)
	if err != nil || packetID != 0x00 {
		return nil, 0, fmt.Errorf("unexpected packet id")
	}
	jsonLen, err := readVarInt(reader)
	if err != nil || jsonLen < 0 || int(jsonLen) > reader.Len() {
		return nil, 0, fmt.Errorf("invalid status payload")
	}
	payload := make([]byte, jsonLen)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, 0, err
	}

	var status slpStatus
	if err := json.Unmarshal(payload, &status); err != nil {
		return nil, 0, err
	}
	return &status, latency, nil
}

func writeFrame(w io.Writer, data []byte) error {
	var buf bytes.Buffer
	writeVarInt(&buf, int32(len(data)))
	buf.Write(data)
	_, err := w.Write(buf.Bytes())
	return err
}

func writeVarInt(w io.Writer, value int32) {
	v := uint32(value)
	for {
		if v&^0x7F == 0 {
			w.Write([]byte{byte(v)})
			return
		}
		w.Write([]byte{byte(v&0x7F | 0x80)})
		v >>= 7
	}
}

func readVarInt(r io.Reader) (int32, error) {
	var value int32
	var position int
	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, err
		}
		value |= int32(buf[0]&0x7F) << position
		if buf[0]&0x80 == 0 {
			return value, nil
		}
		position += 7
		if position >= 32 {
			return 0, fmt.Errorf("varint too big")
		}
	}
}
