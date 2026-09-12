package store

import (
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// crockford is Crockford's base32 alphabet in lowercase: the digits plus the
// letters, less i, l, o, and u, which read ambiguously. Ids use it so they are
// lexically sortable, case-insensitive to type, and hard to misread aloud.
const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// idLen is the number of base32 characters in an id: 8 characters carry 40 bits.
const idLen = 8

// idEpoch is the zero point ids count milliseconds from. 40 bits of
// milliseconds spans about 34.8 years, so ids stay 8 characters until 2054.
// Unix's own epoch would already have overflowed 40 bits.
var idEpoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// idMax is the largest value 8 base32 characters can hold.
const idMax = 1<<40 - 1

var idMu struct {
	sync.Mutex
	last uint64 // the last value handed out, so ids never repeat or go backwards
}

// NewID returns an 8-character id encoding the current millisecond. Ids issued
// within the same millisecond are advanced by one, so a batch of ids generated
// back to back is unique and strictly ascending — which is also what makes them
// sort into creation order as filenames.
func NewID() string {
	return newIDAt(time.Now())
}

func newIDAt(now time.Time) string {
	ms := now.Sub(idEpoch).Milliseconds()
	var v uint64
	if ms > 0 {
		v = uint64(ms)
	}

	idMu.Lock()
	if v <= idMu.last {
		v = idMu.last + 1
	}
	idMu.last = v
	idMu.Unlock()

	return encodeID(v)
}

// encodeID renders v as idLen base32 characters, most significant first.
func encodeID(v uint64) string {
	v &= idMax
	buf := make([]byte, idLen)
	for i := idLen - 1; i >= 0; i-- {
		buf[i] = crockford[v&31]
		v >>= 5
	}
	return string(buf)
}

// shortIDLen is how many characters of an id a short reference carries: the
// tag the TUI prints beside a title and the form a reader types back.
const shortIDLen = 3

// ShortID is the tail of an id, which is the part of it that tells two items
// apart. An id encodes a millisecond, so its leading characters are the high
// bits of the clock: every item created inside the same 9-hour window shares
// its first three, and a store's whole listing can share them. The last three
// are the low bits, which turn over every millisecond.
//
// An id shorter than shortIDLen — there are none NewID makes, but a hand-edited
// file can carry one — is returned whole rather than padded.
func ShortID(id string) string {
	if len(id) <= shortIDLen {
		return id
	}
	return id[len(id)-shortIDLen:]
}

// slugMaxLen caps a slug so a filename stays comfortably inside every
// filesystem's per-component limit once the id and .md suffix are added.
const slugMaxLen = 60

// Slug renders a title as the filename fragment that follows an item's id:
// lowercase, with every run of non-alphanumeric characters collapsed to a
// single hyphen. Letters outside ASCII are kept — they are legal in a filename
// — so a title in another script still produces a readable name.
//
// A title with nothing alphanumeric in it slugs to the empty string; callers
// substitute their own fallback rather than getting a name of bare hyphens.
func Slug(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	pendingHyphen := false
	for _, r := range title {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(unicode.ToLower(r))
		default:
			pendingHyphen = true
		}
	}
	return truncateSlug(b.String())
}

// truncateSlug cuts a slug to slugMaxLen, preferring a hyphen boundary so the
// name ends on a whole word rather than mid-syllable.
func truncateSlug(s string) string {
	if len(s) <= slugMaxLen {
		return s
	}
	cut := s[:slugMaxLen]
	// A byte-wise cut can land inside a multi-byte rune; drop that partial rune
	// rather than leaving invalid UTF-8 in a filename.
	for len(cut) > 0 {
		r, size := utf8.DecodeLastRuneInString(cut)
		if r != utf8.RuneError || size > 1 {
			break
		}
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndexByte(cut, '-'); i > slugMaxLen/2 {
		cut = cut[:i]
	}
	return strings.Trim(cut, "-")
}
