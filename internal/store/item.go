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

// knownKeys are the frontmatter keys Item decodes into typed fields. Everything
// else is left alone in the parsed mapping.
var knownKeys = map[string]bool{
	"id": true, "title": true, "tags": true, "due": true,
	"created": true, "updated": true, "done_at": true,
	"source": true, "context": true,
	"claude_session_name": true, "claude_session_id": true,
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
		if !knownKeys[key.Value] {
			continue // unrecognized, and left exactly where it was
		}
		if err := it.decodeField(key.Value, val); err != nil {
			return nil, err
		}
	}
	return it, nil
}

// decodeField reads one known frontmatter value into its typed field.
func (it *Item) decodeField(key string, val *yaml.Node) error {
	null := val.Tag == "!!null"
	fail := func(err error) error {
		if err == nil {
			return nil
		}
		return fmt.Errorf("item frontmatter key %q: %w", key, err)
	}
	switch key {
	case "id":
		return fail(decodeScalar(val, &it.ID))
	case "title":
		return fail(decodeScalar(val, &it.Title))
	case "source":
		return fail(decodeScalar(val, &it.Source))
	case "context":
		return fail(decodeScalar(val, &it.Context))
	case "claude_session_name":
		return fail(decodeScalar(val, &it.ClaudeSessionName))
	case "claude_session_id":
		return fail(decodeScalar(val, &it.ClaudeSessionID))
	case "tags":
		if null {
			return nil
		}
		if err := val.Decode(&it.Tags); err != nil {
			return fail(err)
		}
	case "due":
		if null {
			return nil
		}
		var d Date
		if err := d.UnmarshalYAML(val); err != nil {
			return fail(err)
		}
		it.Due = &d
	case "created":
		return fail(decodeTime(val, &it.Created))
	case "updated":
		return fail(decodeTime(val, &it.Updated))
	case "done_at":
		if null {
			return nil
		}
		var t time.Time
		if err := decodeTime(val, &t); err != nil {
			return fail(err)
		}
		it.DoneAt = &t
	}
	return nil
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
// mapping to start from and is written in the canonical §7 shape.
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

// keyOrder is the order INTENT §7 gives the keys td owns. It is the shape a
// new item is written in, and the yardstick for where a key that has just
// gained a value belongs in a file td did not write.
var keyOrder = []string{
	"id", "title", "tags", "due", "created", "updated", "done_at",
	"source", "context", "claude_session_name", "claude_session_id",
}

// frontmatter builds the mapping to emit.
func (it *Item) frontmatter() (*yaml.Node, error) {
	if it.parsed == nil {
		return it.skeleton()
	}
	return it.inPlace()
}

// skeleton is the canonical §7 frontmatter, written for an item that came from
// td rather than from a file: every mandatory key, in order, with tags and
// done_at present even when empty so the file shows a person what it can hold.
func (it *Item) skeleton() (*yaml.Node, error) {
	m := &yaml.Node{Kind: yaml.MappingNode}
	put := func(key string, val *yaml.Node) {
		m.Content = append(m.Content, str(key), val)
	}

	put("id", str(it.ID))
	put("title", str(it.Title))
	put("tags", seq(it.Tags))
	if it.Due != nil {
		put("due", dateNode(*it.Due))
	}
	created, err := timeNode(it.Created)
	if err != nil {
		return nil, err
	}
	put("created", created)
	updated, err := timeNode(it.Updated)
	if err != nil {
		return nil, err
	}
	put("updated", updated)
	if it.DoneAt == nil {
		put("done_at", null())
	} else {
		doneAt, err := timeNode(*it.DoneAt)
		if err != nil {
			return nil, err
		}
		put("done_at", doneAt)
	}
	for _, key := range []string{"source", "context", "claude_session_name", "claude_session_id"} {
		if val, err := it.value(key); err != nil {
			return nil, err
		} else if val != nil {
			put(key, val)
		}
	}
	return m, nil
}

// inPlace edits the mapping the item was parsed from: every key td owns takes
// its current value where the file already has that key, a key that has gained
// a value since is inserted where §7 says it belongs, and no key is ever
// removed — a file that says done_at: goes on saying it.
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

	for _, key := range keyOrder {
		val, err := it.value(key)
		if err != nil {
			return nil, err
		}
		if i := at(key); i >= 0 {
			if val == nil {
				val = emptyValue(key)
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
		// Insert after the last key §7 puts ahead of this one that the file
		// actually has, so the addition reads as part of the same block.
		insert := 0
		for _, before := range keyOrder {
			if before == key {
				break
			}
			if i := at(before); i >= 0 {
				insert = i + 2
			}
		}
		rest := append([]*yaml.Node{str(key), val}, m.Content[insert:]...)
		m.Content = append(m.Content[:insert:insert], rest...)
	}
	return &m, nil
}

// value renders one key td owns from the field behind it, or nil when the
// field holds nothing worth a line in the file.
func (it *Item) value(key string) (*yaml.Node, error) {
	switch key {
	case "id":
		return str(it.ID), nil
	case "title":
		return str(it.Title), nil
	case "tags":
		if len(it.Tags) == 0 {
			return nil, nil
		}
		return seq(it.Tags), nil
	case "due":
		if it.Due == nil {
			return nil, nil
		}
		return dateNode(*it.Due), nil
	case "created":
		if it.Created.IsZero() {
			return nil, nil
		}
		return timeNode(it.Created)
	case "updated":
		if it.Updated.IsZero() {
			return nil, nil
		}
		return timeNode(it.Updated)
	case "done_at":
		if it.DoneAt == nil {
			return nil, nil
		}
		return timeNode(*it.DoneAt)
	case "source":
		return optional(it.Source), nil
	case "context":
		return optional(it.Context), nil
	case "claude_session_name":
		return optional(it.ClaudeSessionName), nil
	case "claude_session_id":
		return optional(it.ClaudeSessionID), nil
	}
	return nil, fmt.Errorf("no such frontmatter key %q", key)
}

// optional renders a string field, or nil when it is empty.
func optional(s string) *yaml.Node {
	if s == "" {
		return nil
	}
	return str(s)
}

// emptyValue is what a key already in the file becomes once its field is
// empty: an empty list for tags, and nothing at all for the rest.
func emptyValue(key string) *yaml.Node {
	if key == "tags" {
		return seq(nil)
	}
	return null()
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
