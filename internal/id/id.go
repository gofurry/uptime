package id

import (
	"crypto/rand"
	"encoding/binary"
	"hash/fnv"
	"os"
	"time"
)

func InstanceID(nodeID int64) int64 {
	host, _ := os.Hostname()
	h := fnv.New64a()
	_, _ = h.Write([]byte(host))
	var buf [32]byte
	binary.LittleEndian.PutUint64(buf[0:8], uint64(time.Now().UnixNano()))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(os.Getpid()))
	binary.LittleEndian.PutUint64(buf[16:24], uint64(nodeID))
	if _, err := rand.Read(buf[24:32]); err != nil {
		binary.LittleEndian.PutUint64(buf[24:32], uint64(time.Now().UnixNano()<<7))
	}
	_, _ = h.Write(buf[:])
	return int64(h.Sum64() & 0x7fffffffffffffff)
}
