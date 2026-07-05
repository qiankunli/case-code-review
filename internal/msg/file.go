package msg

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/qiankunli/case-code-review/internal/llm"
)

// File is a typed message for file content that entered the conversation as a
// file_read tool result. It keeps the identity a wire tool_result erases —
// which path, which line range — so the loop can reason about file content as
// content: deduplicate a re-read of the same range, and (later) evict by
// re-derivability when the context tightens (file content is the one thing
// that can always be fetched again).
//
// A File is held by pointer so it can be stubbed IN PLACE: stubbing swaps the
// lowered text for a one-line pointer while keeping the message's position and
// tool_call_id — the 1:1 lowering invariant and the wire protocol's
// call/result pairing both stay intact.
type File struct {
	Path       string
	Start, End int // 1-indexed inclusive line range actually shown
	Total      int // total lines in the file at read time

	wire    llm.Message // the original tool_result (role + ToolCallID preserved)
	stubbed StubReason  // "" = full content
}

// StubReason selects the pointer text a stubbed File lowers to — the model
// must know WHY content vanished: a superseded copy points forward to the
// newer read; an evicted one says how to get the content back.
type StubReason string

const (
	// StubSuperseded: a later read covers this one; the content is below.
	StubSuperseded StubReason = "superseded"
	// StubEvicted: elided under token pressure; re-derivable via file_read.
	StubEvicted StubReason = "evicted"
)

// Lower renders the full content, or — once stubbed — a pointer that keeps the
// message's wire shape (a tool_result stays a paired tool_result, a briefing
// preload stays a user message) while spending no meaningful tokens.
func (f *File) Lower() llm.Message {
	var text string
	switch f.stubbed {
	case StubSuperseded:
		text = fmt.Sprintf("File: %s lines %d-%d — superseded by a later read of the same content below; elided.",
			f.Path, f.Start, f.End)
	case StubEvicted:
		text = fmt.Sprintf("File: %s lines %d-%d — elided to fit the context budget; call file_read again if you still need it.",
			f.Path, f.Start, f.End)
	default:
		return f.wire
	}
	if f.wire.Role == "tool" {
		return llm.NewToolResultMessage(f.wire.ToolCallID, text)
	}
	return llm.NewTextMessage(f.wire.Role, text)
}

// Stub elides the content with the given reason (idempotent; the first reason
// wins — a superseded copy staying "superseded" under later eviction pressure
// keeps its forward pointer meaningful).
func (f *File) Stub(reason StubReason) {
	if f.stubbed == "" {
		f.stubbed = reason
	}
}

// Stubbed reports whether the content has been elided.
func (f *File) Stubbed() bool { return f.stubbed != "" }

// Covers reports whether f's range contains g's range of the same path — the
// dedup precondition: everything g shows, f shows too.
func (f *File) Covers(g *File) bool {
	return f.Path == g.Path && f.Start <= g.Start && f.End >= g.End
}

// fileReadHeader matches the file_read tool's response header, which is the
// tool's OUTPUT CONTRACT (internal/tool/file_read.go): a "File:" line with the
// path and total, then a LINE_RANGE line with the displayed range. Parsing the
// result (rather than the tool-call arguments) keeps this independent of
// default-filling logic — the header states what was actually shown.
var fileReadHeader = regexp.MustCompile(`^File: (.+) \(Total lines: (\d+)\)\nIS_TRUNCATED: (?:true|false)\nLINE_RANGE: (\d+)-(\d+)\n`)

// FileReadToolName is the tool whose results are promoted to File messages.
const FileReadToolName = "file_read"

// NewFile builds a File whose content entered the conversation OUTSIDE the
// tool protocol — a briefing preload carried as a user-role message. Same
// identity, same dedup/evict participation; only the wire shape differs (no
// tool_call pairing to preserve).
func NewFile(path string, start, end, total int, content string) *File {
	return &File{
		Path:  path,
		Start: start,
		End:   end,
		Total: total,
		wire:  llm.NewTextMessage("user", content),
	}
}

// FileFromToolResult promotes a file_read tool result into a *File. Returns
// (nil, false) for other tools or when the result doesn't carry the expected
// header (errors, truncation notices with unexpected shapes) — the caller
// falls back to Raw and nothing is lost.
func FileFromToolResult(toolName, result string, wire llm.Message) (*File, bool) {
	if toolName != FileReadToolName {
		return nil, false
	}
	m := fileReadHeader.FindStringSubmatch(result)
	if m == nil {
		return nil, false
	}
	total, err1 := strconv.Atoi(m[2])
	start, err2 := strconv.Atoi(m[3])
	end, err3 := strconv.Atoi(m[4])
	if err1 != nil || err2 != nil || err3 != nil || start < 1 || end < start {
		return nil, false
	}
	return &File{
		Path:  strings.TrimSpace(m[1]),
		Start: start,
		End:   end,
		Total: total,
		wire:  wire,
	}, true
}

// DedupFiles stubs every earlier un-stubbed File whose range is covered by a
// LATER read of the same path — the model keeps the newest copy (nearest to
// the conversation tail, least likely compressed away) and pays for the
// content once. Line-shift safety: reads at different times could see
// different file states, but within one review loop the workspace/ref is
// fixed, so same path + covered range ⇒ same content.
func DedupFiles(messages []Msg) (stubbed int) {
	for i := len(messages) - 1; i >= 0; i-- {
		newer, ok := messages[i].(*File)
		if !ok || newer.Stubbed() {
			continue
		}
		for j := range i {
			older, ok := messages[j].(*File)
			if !ok || older.Stubbed() {
				continue
			}
			if newer.Covers(older) {
				older.Stub(StubSuperseded)
				stubbed++
			}
		}
	}
	return stubbed
}
