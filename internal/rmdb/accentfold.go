package rmdb

// FoldAccents strips diacritics from accented Latin letters, matching
// RootsMagic's own transformation for NameTable.SurnameMP/GivenMP/
// NicknameMP from their corresponding Surname/Given/Nickname -- confirmed
// against the one real example this project has captured
// ("Brontë" -> "Bronte") and, beyond that one example, backed by
// Unicode's own well-defined NFD decomposition rather than a further
// guess (see accentFoldTable's own comment for the exact scope and its
// limits).
//
// Characters with no entry in accentFoldTable (already-plain ASCII,
// unrecognized/non-Latin script, or a ligature/special letter with no
// base-plus-accent decomposition at all) pass through unchanged.
func FoldAccents(s string) string {
	runes := []rune(s)
	changed := false
	for i, r := range runes {
		if folded, ok := accentFoldTable[r]; ok {
			runes[i] = folded
			changed = true
		}
	}
	if !changed {
		return s
	}
	return string(runes)
}
