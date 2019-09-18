package test

import (
	"crypto/rand"
	log "github.com/sirupsen/logrus"
	r "math/rand"
	"time"
)

func init() {
	r.Seed(time.Now().UTC().UnixNano())
}

const (
	// 52 possibilities
	letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

	// 6 bits to represent 64 possibilities / indexes
	letterIdxBits = 6

	// All 1-bits, as many as letterIdxBits
	letterIdxMask = 1<<letterIdxBits - 1
)

// SecureRandomAlphaString generates a  random alpha of the requested length
func SecureRandomAlphaString(length int) string {
	result := make([]byte, length)
	bufferSize := int(float64(length) * 1.3)
	for i, j, randomBytes := 0, 0, []byte{}; i < length; j++ {
		if j%bufferSize == 0 {
			randomBytes = SecureRandomBytes(bufferSize)
		}
		if idx := int(randomBytes[j%length] & letterIdxMask); idx < len(letterBytes) {
			result[i] = letterBytes[idx]
			i++
		}
	}

	return string(result)
}

// SecureRandomBytes returns the requested number of bytes using crypto/rand
func SecureRandomBytes(length int) []byte {
	var randomBytes = make([]byte, length)
	_, err := rand.Read(randomBytes)
	if err != nil {
		log.Fatal("Unable to generate random bytes")
	}
	return randomBytes
}
