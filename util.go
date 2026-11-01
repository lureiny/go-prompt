package prompt

import "unicode/utf8"

// runeIndexToByteOffset converts a rune index (as reported by bubbles
// textinput.Position) into a byte offset into s (as used for slicing s).
func runeIndexToByteOffset(s string, runeIndex int) int {
	if runeIndex <= 0 {
		return 0
	}
	n := 0
	for i := range s {
		if n == runeIndex {
			return i
		}
		n++
	}
	return len(s)
}

// completeUTF8Len returns the length of the longest prefix of b that ends on a
// UTF-8 rune boundary. It excludes a trailing byte sequence that begins a
// multibyte rune whose remaining bytes have not arrived yet, so callers can hold
// those bytes back until the rest of the rune is available.
func completeUTF8Len(b []byte) int {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if i+utf8RuneLen(b[i]) > len(b) {
				return i // trailing rune is incomplete — cut before it
			}
			return len(b)
		}
	}
	return len(b)
}

// utf8RuneLen returns the encoded length implied by a UTF-8 leading byte.
func utf8RuneLen(lead byte) int {
	switch {
	case lead < 0x80:
		return 1
	case lead < 0xE0:
		return 2
	case lead < 0xF0:
		return 3
	default:
		return 4
	}
}

// byteOffsetToRuneIndex converts a byte offset into s into a rune index
// (as expected by bubbles textinput.SetCursor).
func byteOffsetToRuneIndex(s string, byteOffset int) int {
	if byteOffset <= 0 {
		return 0
	}
	if byteOffset >= len(s) {
		return utf8.RuneCountInString(s)
	}
	return utf8.RuneCountInString(s[:byteOffset])
}
