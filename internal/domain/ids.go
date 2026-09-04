package domain

import (
	"crypto/rand"
	"math/big"
)

const idAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// NewID returns a short random identifier prefixed with the given tag
// (e.g. "c_" for collections, "f_" for folders, "r_" for requests, "e_"
// for environments), matching the id shapes used in the on-disk
// collection/environment JSON. IDs only need to be unique within a single
// file, so six characters is plenty.
func NewID(prefix string) string {
	b := make([]byte, 6)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(idAlphabet))))
		if err != nil {
			panic(err)
		}
		b[i] = idAlphabet[n.Int64()]
	}
	return prefix + string(b)
}
