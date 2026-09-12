package store

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// fence is the line that opens and closes an item's YAML frontmatter block.
const fence = "---"

// dateLayout is the wire format of the due field: a calendar date, no time.
const dateLayout = "2006-01-02"

var (
	// ErrNoFrontmatter is returned when a file does not open with a --- fence.
	ErrNoFrontmatter = errors.New("item has no frontmatter: file must start with ---")
	// ErrUnterminatedFrontmatter is returned when the opening fence has no match.
	ErrUnterminatedFrontmatter = errors.New("item frontmatter is unterminated: no closing ---")
)

// Date is a calendar date with no time component, used for the due field. It
// accepts both a quoted string and YAML's native timestamp scalar on the way
// in, and always writes 2006-01-02 on the way out.
type Date struct {
	time.Time
}

// ParseDate reads a YYYY-MM-DD date.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return Date{t}, nil
}

func (d Date) String() string { return d.Format(dateLayout) }

// UnmarshalYAML reads a date from either a !!str or a !!timestamp scalar. Both
// carry the raw text in Value, so only the text is parsed.
func (d *Date) UnmarshalYAML(n *yaml.Node) error {
	if n.Tag == "!!null" || n.Value == "" {
		return nil
	}
	// A bare 2026-09-10 resolves to !!timestamp; a quoted one to !!str. Either
	// way the date text is n.Value, and anything longer is a datetime we reject.
	parsed, err := ParseDate(n.Value)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Item is one todo: the frontmatter fields td owns, the markdown body, and the
// frontmatter keys td does not recognize, kept so a rewrite never drops them.
type Item struct {
	ID                string
	Title             string
	Tags              []string
	Due               *Date
	Created           time.Time
	Updated           time.Time
	DoneAt            *time.Time
	Source            string
	Context           string
	ClaudeSessionName string
	ClaudeSessionID   string

	// Body is the markdown after the closing fence, with no leading blank line
	// and at most one trailing newline.
	Body string

	// parsed is the frontmatter mapping this item was read from, kept whole so
	// a rewrite edits the file's own shape rather than rebuilding it: keys
	// stay where their author put them, keys td does not recognize are never
	// moved, and a key the file never had is not invented. An item td made
	// itself has none, and is written in the canonical shape instead.
	parsed *yaml.Node
}

// Done reports whether the item has been completed.
func (it *Item) Done() bool { return it.DoneAt != nil }

// fieldSpec is one frontmatter key td owns: how to read it into the Item, how
// to write it back, and what the key looks like with nothing in it. The table
// below is the only place a key is registered — the decoder, the key order and
// both writers all derive from it.
//
// The single registration is the point. A key's decoder, its encoder, its
// position in the file and its empty form used to be four separate literals,
// and a field added to three of them failed silently: it round-tripped through
// a file somebody wrote by hand, so nothing ever errored, and it vanished from
// every item td created itself.
type fieldSpec struct {
	key string

	// decode reads the file's value into the field behind the key. The caller
	// names the key in the error, so a decoder reports only what went wrong.
	decode func(it *Item, n *yaml.Node) error

	// encode renders the field, or a nil node when it holds nothing worth a
	// line in the file.
	encode func(it *Item) (*yaml.Node, error)

	// inSkeleton keeps the key in the canonical shape even when encode has
	// nothing to say, so a file td wrote shows a person what it can hold. It
	// says nothing about a file td did not write: inPlace never invents a key
	// the file does not already have.
	inSkeleton bool

	// empty is what the key looks like once its field holds nothing — both for
	// a key the file already has and for an inSkeleton key with nothing to
	// say. nil means an empty scalar.
	empty func() *yaml.Node
}

// fields are the frontmatter keys td owns, in the order td writes them.
var fields = []fieldSpec{
	{
		key:        "id",
		decode:     func(it *Item, n *yaml.Node) error { return decodeScalar(n, &it.ID) },
		encode:     func(it *Item) (*yaml.Node, error) { return str(it.ID), nil },
		inSkeleton: true,
	},
	{
		key:        "title",
		decode:     func(it *Item, n *yaml.Node) error { return decodeScalar(n, &it.Title) },
		encode:     func(it *Item) (*yaml.Node, error) { return str(it.Title), nil },
		inSkeleton: true,
	},
	{
		key: "tags",
		decode: func(it *Item, n *yaml.Node) error {
			if n.Tag == "!!null" {
				return nil
			}
			return n.Decode(&it.Tags)
		},
		encode: func(it *Item) (*yaml.Node, error) {
			if len(it.Tags) == 0 {
				return nil, nil
			}
			return seq(it.Tags), nil
		},
		inSkeleton: true,
		// The one key whose emptiness is a list rather than a bare scalar.
		empty: func() *yaml.Node { return seq(nil) },
	},
	{
		key: "due",
		decode: func(it *Item, n *yaml.Node) error {
			if n.Tag == "!!null" {
				return nil
			}
			var d Date
			if err := d.UnmarshalYAML(n); err != nil {
				return err
			}
			it.Due = &d
			return nil
		},
		encode: func(it *Item) (*yaml.Node, error) {
			if it.Due == nil {
				return nil, nil
			}
			return dateNode(*it.Due), nil
		},
	},
	{
		key:    "created",
		decode: func(it *Item, n *yaml.Node) error { return decodeTime(n, &it.Created) },
		encode: func(it *Item) (*yaml.Node, error) {
			if it.Created.IsZero() {
				return nil, nil
			}
			return timeNode(it.Created)
		},
		inSkeleton: true,
	},
	{
		key:    "updated",
		decode: func(it *Item, n *yaml.Node) error { return decodeTime(n, &it.Updated) },
		encode: func(it *Item) (*yaml.Node, error) {
			if it.Updated.IsZero() {
				return nil, nil
			}
			return timeNode(it.Updated)
		},
		inSkeleton: true,
	},
	{
		key: "done_at",
		decode: func(it *Item, n *yaml.Node) error {
			if n.Tag == "!!null" {
				return nil
			}
			var t time.Time
			if err := decodeTime(n, &t); err != nil {
				return err
			}
			it.DoneAt = &t
			return nil
		},
		encode: func(it *Item) (*yaml.Node, error) {
			if it.DoneAt == nil {
				return nil, nil
			}
			return timeNode(*it.DoneAt)
		},
		inSkeleton: true,
	},
	{
		key:    "source",
		decode: func(it *Item, n *yaml.Node) error { return decodeScalar(n, &it.Source) },
		encode: func(it *Item) (*yaml.Node, error) { return optional(it.Source), nil },
	},
	{
		key:    "context",
		decode: func(it *Item, n *yaml.Node) error { return decodeScalar(n, &it.Context) },
		encode: func(it *Item) (*yaml.Node, error) { return optional(it.Context), nil },
	},
	{
		key:    "claude_session_name",
		decode: func(it *Item, n *yaml.Node) error { return decodeScalar(n, &it.ClaudeSessionName) },
		encode: func(it *Item) (*yaml.Node, error) { return optional(it.ClaudeSessionName), nil },
	},
	{
		key:    "claude_session_id",
		decode: func(it *Item, n *yaml.Node) error { return decodeScalar(n, &it.ClaudeSessionID) },
		encode: func(it *Item) (*yaml.Node, error) { return optional(it.ClaudeSessionID), nil },
	},
}

// fieldByKey indexes the table, so reading a file matches its keys rather than
// walking the table once per key.
var fieldByKey = func() map[string]fieldSpec {
	m := make(map[string]fieldSpec, len(fields))
	for _, f := range fields {
		m[f.key] = f
	}
	return m
}()

// keyOrder is the order td writes the keys it owns, read off the table. It is
// the shape a new item is written in, and the yardstick for where a key that
// has just gained a value belongs in a file td did not write.
var keyOrder = func() []string {
	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.key
	}
	return keys
}()

// emptyNode is what the key looks like with nothing in it.
func (f fieldSpec) emptyNode() *yaml.Node {
	if f.empty == nil {
		return null()
	}
	return f.empty()
}

// ParseItem reads one item file: a --- fenced YAML frontmatter block followed
// by a markdown body.
func ParseItem(b []byte) (*Item, error) {
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, fence+"\n") {
		return nil, ErrNoFrontmatter
	}
	rest := text[len(fence)+1:]

	end, after, ok := findFenceLine(rest)
	if !ok {
		return nil, ErrUnterminatedFrontmatter
	}
	frontmatter, body := rest[:end], rest[after:]

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(frontmatter), &doc); err != nil {
		return nil, fmt.Errorf("item frontmatter is not valid YAML: %w", err)
	}

	it := &Item{Body: normalizeBody(body)}
	if len(doc.Content) == 0 {
		return it, nil // an empty frontmatter block is a valid, if useless, item
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("item frontmatter must be a YAML mapping")
	}
	it.parsed = root
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		f, ok := fieldByKey[key.Value]
		if !ok {
			continue // unrecognized, and left exactly where it was
		}
		if err := f.decode(it, val); err != nil {
			return nil, fmt.Errorf("item frontmatter key %q: %w", key.Value, err)
		}
	}
	return it, nil
}

// decodeScalar reads a scalar into a string, treating null as the empty string
// and accepting a value YAML would otherwise resolve to a bool or a number.
func decodeScalar(n *yaml.Node, dst *string) error {
	if n.Tag == "!!null" {
		*dst = ""
		return nil
	}
	if n.Kind != yaml.ScalarNode {
		return errors.New("want a scalar")
	}
	*dst = n.Value
	return nil
}

// decodeTime reads an RFC 3339 timestamp, whether it was written bare or quoted.
func decodeTime(n *yaml.Node, dst *time.Time) error {
	if n.Tag == "!!null" || n.Value == "" {
		*dst = time.Time{}
		return nil
	}
	t, err := time.Parse(time.RFC3339, n.Value)
	if err != nil {
		return fmt.Errorf("invalid timestamp %q: want RFC 3339", n.Value)
	}
	*dst = t
	return nil
}

// Marshal renders the item back to file bytes.
//
// An item read from a file is written back through its own parsed mapping, so
// a file somebody wrote by hand keeps its shape: its keys stay in its order,
// keys td does not recognize are untouched, and an optional key the file never
// had is added only once it has something to say. An item td created has no
// mapping to start from and is written in the canonical shape instead, which
// TestItemTdCreatedStillCarriesTheSkeleton pins byte for byte.
func (it *Item) Marshal() ([]byte, error) {
	m, err := it.frontmatter()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString(fence + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return nil, fmt.Errorf("encoding item frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encoding item frontmatter: %w", err)
	}
	buf.WriteString(fence + "\n")
	if it.Body != "" {
		buf.WriteString("\n")
		buf.WriteString(it.Body)
	}
	return buf.Bytes(), nil
}

// frontmatter builds the mapping to emit.
func (it *Item) frontmatter() (*yaml.Node, error) {
	if it.parsed == nil {
		return it.skeleton()
	}
	return it.inPlace()
}

// skeleton is the canonical frontmatter, written for an item that came from td
// rather than from a file: the keys the table marks inSkeleton are present even
// when empty, so the file shows a person what it can hold, and every other key
// appears only once it has something to say.
func (it *Item) skeleton() (*yaml.Node, error) {
	m := &yaml.Node{Kind: yaml.MappingNode}
	for _, f := range fields {
		val, err := f.encode(it)
		if err != nil {
			return nil, err
		}
		if val == nil {
			if !f.inSkeleton {
				continue
			}
			val = f.emptyNode()
		}
		m.Content = append(m.Content, str(f.key), val)
	}
	return m, nil
}

// inPlace edits the mapping the item was parsed from: every key td owns takes
// its current value where the file already has that key, a key that has gained
// a value since is inserted where the table says it belongs, and no key is ever
// removed — a file that says done_at: goes on saying it. A key the file never
// had and still has nothing to say is not invented here, whatever the table
// says about the skeleton.
func (it *Item) inPlace() (*yaml.Node, error) {
	m := *it.parsed
	m.Content = append([]*yaml.Node(nil), it.parsed.Content...)

	at := func(key string) int {
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == key {
				return i
			}
		}
		return -1
	}

	for _, f := range fields {
		val, err := f.encode(it)
		if err != nil {
			return nil, err
		}
		if i := at(f.key); i >= 0 {
			if val == nil {
				val = f.emptyNode()
			}
			// The key node keeps any comment above it; the value node's own
			// trailing comment has to be carried onto its replacement.
			old := m.Content[i+1]
			val.LineComment, val.FootComment = old.LineComment, old.FootComment
			m.Content[i+1] = val
			continue
		}
		if val == nil {
			continue // absent from the file and still nothing to say
		}
		// Insert after the last key td orders ahead of this one that the file
		// actually has, so the addition reads as part of the same block.
		insert := 0
		for _, before := range keyOrder {
			if before == f.key {
				break
			}
			if i := at(before); i >= 0 {
				insert = i + 2
			}
		}
		rest := append([]*yaml.Node{str(f.key), val}, m.Content[insert:]...)
		m.Content = append(m.Content[:insert:insert], rest...)
	}
	return &m, nil
}

// optional renders a string field, or nil when it is empty.
func optional(s string) *yaml.Node {
	if s == "" {
		return nil
	}
	return str(s)
}

// str builds a scalar node explicitly tagged as a string, so the emitter quotes
// anything that would otherwise read back as a number, bool, or timestamp.
func str(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func null() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: ""}
}

// seq builds a flow-style sequence, so tags stay on one line.
func seq(vals []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, v := range vals {
		n.Content = append(n.Content, str(v))
	}
	return n
}

// dateNode renders a due date the way a person writes one: a bare 2006-01-02,
// which YAML resolves to a timestamp and Date.UnmarshalYAML reads back. Quoting
// it would make td's own files differ from a hand-written one for no gain.
func dateNode(d Date) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: d.String()}
}

// timeNode renders a timestamp in UTC, so ordering is textual as well as
// chronological and two machines write the same bytes.
func timeNode(t time.Time) (*yaml.Node, error) {
	if t.IsZero() {
		return null(), nil
	}
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!timestamp",
		Value: t.UTC().Format(time.RFC3339),
	}, nil
}

// findFenceLine locates the closing fence in text, returning the index the
// frontmatter ends at and the index the body starts at.
func findFenceLine(text string) (end, after int, ok bool) {
	for i := 0; i < len(text); {
		lineEnd := strings.IndexByte(text[i:], '\n')
		var line string
		if lineEnd < 0 {
			line, lineEnd = text[i:], len(text)-i
		} else {
			line = text[i : i+lineEnd]
			lineEnd++ // consume the newline
		}
		if strings.TrimRight(line, " \t") == fence {
			return i, i + lineEnd, true
		}
		i += lineEnd
	}
	return 0, 0, false
}

// normalizeBody strips the blank line that separates the frontmatter from the
// body and collapses trailing newlines to one, so a rewrite is a no-op.
func normalizeBody(body string) string {
	body = strings.TrimLeft(body, "\n")
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return ""
	}
	return body + "\n"
}
