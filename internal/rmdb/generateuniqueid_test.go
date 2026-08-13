package rmdb

import (
	"encoding/hex"
	"strings"
	"testing"
)

// realUniqueIDs are 21 genuine UniqueID values pulled directly from
// RootsMagic-generated data: 7 from the first captured golden files for
// this feature, and 14 more from a full PersonTable snapshot of the
// Brontë family test database (every person that database ever had).
// Not constructed or guessed -- see GenerateUniqueID's own comment.
var realUniqueIDs = []string{
	"911B87B54CCE4839B167F5EC6C13532977DE",
	"B0992F401A0B4CD696507F6CD28B760AADCD",
	"4A779410A3DF463BB0B4179560E314673620",
	"FFA55DC9608945CFB7F1AF2CBA63A6DEEBC2",
	"E9ADE2B48C954D6181D51D8AFFE2AB911559",
	"2EDAC48C5D2D4AD285F365DB41BD6C4363D3",
	"9C70D5D7BC634C1C87FBCF4F928162A8FC97",
	"5B4249B80B6F4F60B51B5FB8DB27CF6CEBC5",
	"007D57A25A484C45BFD4A2A7B0505DD3B514",
	"1EE832BDF11F4B4CA05C5240B95EBE4F4E81",
	"7C062B1467564B4E86CD6FBCE368D99E57CF",
	"4F8A1965BFE14D419D2ED585D6F8AF3C630A",
	"0317D51236BD4A129ADAAEDE343F4F1C2EBD",
	"8395F76D8D834466A22E835ABA8BFC688C80",
}

func TestGenerateUniqueIDChecksumMatchesRealData(t *testing.T) {
	for _, full := range realUniqueIDs {
		if len(full) != 36 {
			t.Fatalf("test data itself malformed: %q is %d chars, want 36", full, len(full))
		}
		core, wantChecksum := full[:32], full[32:]
		coreBytes, err := hex.DecodeString(core)
		if err != nil {
			t.Fatalf("decoding core %q: %v", core, err)
		}
		got := rmChecksum(coreBytes)
		if got != wantChecksum {
			t.Errorf("%s: rmChecksum(core) = %q, want %q", full, got, wantChecksum)
		}
	}
}

func TestGenerateUniqueIDProducesValidFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id, err := GenerateUniqueID()
		if err != nil {
			t.Fatalf("GenerateUniqueID: %v", err)
		}
		if len(id) != 36 {
			t.Fatalf("GenerateUniqueID produced %q, length %d, want 36", id, len(id))
		}
		if strings.ToUpper(id) != id {
			t.Errorf("GenerateUniqueID produced %q, expected all-uppercase hex", id)
		}
		core := id[:32]
		if core[12] != '4' {
			t.Errorf("GenerateUniqueID produced %q, version nibble %q, want '4'", id, core[12])
		}
		v := core[16]
		if v != '8' && v != '9' && v != 'A' && v != 'B' {
			t.Errorf("GenerateUniqueID produced %q, variant nibble %q, want one of 8/9/A/B", id, v)
		}
		coreBytes, err := hex.DecodeString(core)
		if err != nil {
			t.Fatalf("decoding generated core %q: %v", core, err)
		}
		wantChecksum := rmChecksum(coreBytes)
		if id[32:] != wantChecksum {
			t.Errorf("GenerateUniqueID produced %q, checksum %q, want %q", id, id[32:], wantChecksum)
		}
		if seen[id] {
			t.Fatalf("GenerateUniqueID produced a duplicate across 1000 calls: %q", id)
		}
		seen[id] = true
	}
}
