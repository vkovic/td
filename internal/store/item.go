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

	// residue holds unrecognized frontmatter as alternating key/value nodes, in
	// the order they were read. Marshal re-emits them after the known keys.
	residue []*yaml.Node
}

// Done reports whether the item has been completed.
func (it *Item) Done() bool { return it.DoneAt != nil }

// knownKeys are the frontmatter keys Item decodes into typed fields. Everything
// else goes to residue.
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
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		if !knownKeys[key.Value] {
			it.residue = append(it.residue, key, val)
			continue
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

// Marshal renders the item back to file bytes in td's canonical shape: known
// keys in a fixed order, unrecognized keys after them, then the body.
func (it *Item) Marshal() ([]byte, error) {
	m := &yaml.Node{Kind: yaml.MappingNode}
	put := func(key string, val *yaml.Node) {
		m.Content = append(m.Content, str(key), val)
	}

	put("id", str(it.ID))
	put("title", str(it.Title))
	put("tags", seq(it.Tags))
	if it.Due != nil {
		put("due", str(it.Due.String()))
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
	for _, f := range []struct {
		key string
		val string
	}{
		{"source", it.Source},
		{"context", it.Context},
		{"claude_session_name", it.ClaudeSessionName},
		{"claude_session_id", it.ClaudeSessionID},
	} {
		if f.val != "" {
			put(f.key, str(f.val))
		}
	}
	m.Content = append(m.Content, it.residue...)

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
