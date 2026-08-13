package rmdb

import (
	"crypto/rand"
	"fmt"
)

// GenerateUniqueID produces a new RootsMagic-format UniqueID for a
// newly-created record (currently: PersonTable.UniqueID -- see writes.go's
// CreatePerson): a standard 128-bit version 4 UUID, formatted as 32
// uppercase hex characters with the hyphens stripped, followed by a
// 4-character checksum.
//
// The checksum is a Fletcher-16-style running sum over the 16 raw UUID
// bytes (not the hex characters): sum1 accumulates each byte mod 256;
// sum2 accumulates the running sum1 mod 256 at each step; the result is
// formatted as two hex bytes, sum1 then sum2. This isn't guessed or
// derived from the (subtly incorrect -- confirmed a transcription error
// in its own algebraic description of the second byte) GEDCOM 7 UID
// algorithm it's modeled on; it's transcribed directly from a community
// member's own SQL implementation
// (https://sqlitetoolsforrootsmagic.com/forum/topic/uniqueid-in-persontable/#postid-1800),
// and independently confirmed here against 21 real UniqueIDs pulled from
// real RootsMagic-generated data (7 from the first captured golden files,
// 14 more from a full snapshot of every person in the Brontë test
// database) -- every single one checksums correctly against this
// implementation. See TestGenerateUniqueIDChecksumMatchesRealData.
func GenerateUniqueID() (string, error) {
	var uuidBytes [16]byte
	if _, err := rand.Read(uuidBytes[:]); err != nil {
		return "", fmt.Errorf("generating random bytes for UniqueID: %w", err)
	}
	// RFC 4122 version 4 (random) and variant bits.
	uuidBytes[6] = (uuidBytes[6] & 0x0f) | 0x40
	uuidBytes[8] = (uuidBytes[8] & 0x3f) | 0x80

	core := fmt.Sprintf("%X", uuidBytes[:])
	return core + rmChecksum(uuidBytes[:]), nil
}

// rmChecksum computes the 4-character checksum suffix described in
// GenerateUniqueID's own comment, given the raw (already-generated) 16
// UUID bytes. Split out from GenerateUniqueID so it can be tested
// directly against known real (core, checksum) pairs, independent of
// random generation.
func rmChecksum(data []byte) string {
	var sum1, sum2 byte
	for _, b := range data {
		sum1 += b
		sum2 += sum1
	}
	return fmt.Sprintf("%02X%02X", sum1, sum2)
}
