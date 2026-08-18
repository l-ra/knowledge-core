package datatype

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IsIRI reports whether s looks like an absolute IRI/URI that we accept
// as a public resource identifier. We allow http(s) and urn.
func IsIRI(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch u.Scheme {
	case "http", "https":
		return u.Host != ""
	case "urn":
		return u.Opaque != ""
	default:
		return false
	}
}

func ValidateIdentifier(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("identifier is required")
	}
	if IsIRI(id) {
		return nil
	}
	if _, err := ParsePublicStatementID(id); err == nil {
		return nil
	}
	if _, _, err := ParsePublicGraphID(id); err == nil {
		return nil
	}
	if len(id) > 1 && strings.HasPrefix(id, "R") {
		return nil
	}
	return fmt.Errorf("invalid identifier %q", id)
}

const snowflakeEpochMs uint64 = 1735689600000 // 2025-01-01T00:00:00Z

var snowflakeState = struct {
	mu       sync.Mutex
	lastMs   uint64
	sequence uint16
	nodeID   uint16
}{
	nodeID: initSnowflakeNodeID(),
}

func initSnowflakeNodeID() uint16 {
	var buf [2]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return uint16(time.Now().UnixNano()) & 0x03ff
	}
	return binary.BigEndian.Uint16(buf[:]) & 0x03ff
}

func nextSnowflake() uint64 {
	snowflakeState.mu.Lock()
	defer snowflakeState.mu.Unlock()

	nowMs := uint64(time.Now().UTC().UnixMilli())
	if nowMs < snowflakeEpochMs {
		nowMs = snowflakeEpochMs
	}
	ts := nowMs - snowflakeEpochMs
	if nowMs == snowflakeState.lastMs {
		snowflakeState.sequence = (snowflakeState.sequence + 1) & 0x0fff
		if snowflakeState.sequence == 0 {
			for nowMs <= snowflakeState.lastMs {
				nowMs = uint64(time.Now().UTC().UnixMilli())
			}
			ts = nowMs - snowflakeEpochMs
		}
	} else {
		snowflakeState.sequence = 0
	}
	snowflakeState.lastMs = nowMs
	return (ts << 22) | (uint64(snowflakeState.nodeID) << 12) | uint64(snowflakeState.sequence)
}

func NewFallbackIRILocal(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "id"
	}
	return prefix + "_" + strings.ToLower(strconv.FormatUint(nextSnowflake(), 36))
}

func PackageDisplayID(packageCode, iriLocal, publicID string) string {
	if packageCode != "" && iriLocal != "" {
		return packageCode + ":" + iriLocal
	}
	if packageCode != "" && publicID != "" && !IsIRI(publicID) {
		return packageCode + ":" + publicID
	}
	if iriLocal != "" {
		return iriLocal
	}
	if publicID != "" {
		if IsIRI(publicID) {
			if i := strings.LastIndexAny(publicID, "/#"); i >= 0 && i+1 < len(publicID) {
				return publicID[i+1:]
			}
		}
		return publicID
	}
	return ""
}
