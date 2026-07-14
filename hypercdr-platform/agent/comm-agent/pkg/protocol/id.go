package protocol

import (
	"crypto/rand"
	"encoding/hex"
)

func newMessageID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b[:])
}
