// Package termsafe removes from the text of the guest what a terminal must not get.
//
// The guest controls the bytes of its console and the output of its commands. They go to
// the terminal of the user and to the logs. Thus the filter removes all control characters
// and escape sequences, also those that set a color. Only newlines and tabs stay. Without
// that, the guest can change the title of the terminal or write to the clipboard.
//
// The guest must also not write lines that look like the messages of the sandbox-vm
// process. Thus a line that starts with Mark gets a space before it. Characters that a
// terminal does not show must not hide that start: the filter removes them at the start of
// a line and in a start of Mark. A long line that the terminal wraps can still show Mark
// at the start of a row.
//
// For the output of a command, a carriage return in a line can also replace the line, as on
// a terminal (Filter.Lines). Then a progress counter shows only its last value. The console
// does not use this: in its log, the guest must not remove text that it wrote.
package termsafe

import (
	"bytes"
	"io"
	"unicode"
	"unicode/utf8"
)

// Mark is the start of each message of the sandbox-vm process.
const Mark = "[sandbox-vm]"

// Copy copies text of the guest from r to w until r ends.
//
// This also removes the carriage returns with which the guest ends its lines. The process
// manager shows each of them as one more line break.
func Copy(w io.Writer, r io.Reader) {
	copyFilter(w, r, &Filter{})
}

// CopyLines is Copy for the output of a command: a carriage return replaces the line
// (Filter.Lines).
func CopyLines(w io.Writer, r io.Reader) {
	copyFilter(w, r, &Filter{Lines: true})
}

func copyFilter(w io.Writer, r io.Reader, filter *Filter) {
	buf := make([]byte, 32<<10)
	var out []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = filter.AppendSafe(out[:0], buf[:n])
			if _, err := w.Write(out); err != nil {
				return
			}
		}
		if err != nil {
			// The input ended, and there is no caller to tell about an output that also ended.
			_, _ = w.Write(filter.End(out[:0]))
			return
		}
	}
}

// Writer writes to w only the bytes that a terminal can get.
type Writer struct {
	w      io.Writer
	filter Filter
	out    []byte
}

// NewWriter returns a Writer for the output of one command of the guest.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

func (s *Writer) Write(p []byte) (int, error) {
	s.out = s.filter.AppendSafe(s.out[:0], p)
	if _, err := s.w.Write(s.out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Filter removes the bytes that a terminal must not get. A sequence or a character can
// start in one block of bytes and end in the subsequent block, thus the filter keeps its
// position between the blocks.
//
// The filter has two parts. The first part finds the escape sequences and removes each of
// them fully, thus no remainder of a sequence stays in the text. The second part
// (appendText) is the protection: it lets only permitted characters through. If the first
// part does not know a sequence, the second part removes its ESC, and the remainder is
// only text.
type Filter struct {
	// A carriage return that a character other than a newline follows removes the line
	// before it, as on a terminal. The filter holds the line until its end. A line of more
	// than maxLine bytes goes to the output, and a carriage return no longer removes it.
	Lines bool

	state int
	// The bytes of a character of more than one byte that is not complete.
	pend []byte
	// The output is in a line, after the first byte of the line.
	inLine bool
	// The number of bytes of Mark at the start of a line that are not in the output yet.
	held int
	// For Lines: the line that is not complete, the line went to the output (it is too
	// long), and a carriage return came last.
	line     []byte
	overflow bool
	cr       bool
}

// maxLine is the longest line that a Filter with Lines holds.
const maxLine = 64 << 10

// The positions of Filter.
const (
	text      = iota
	escape    // after ESC
	skip      // in a sequence, before its last byte
	str       // in a string (ESC ] and others), before BEL, ESC \ or the end of the line
	stringEnd // after ESC in a string
)

// AppendSafe appends the bytes of src that a terminal can get to dst.
func (f *Filter) AppendSafe(dst, src []byte) []byte {
	// The line that the filter holds is again in dst while the filter works.
	base := len(dst)
	dst = append(dst, f.line...)
	f.line = f.line[:0]
	for _, b := range src {
		if f.cr && b != '\r' {
			f.cr = false
			if b != '\n' {
				dst = f.replaceLine(dst, base)
			}
		}
		// Each case ends with continue, or the byte goes to the rules for text.
		switch f.state {
		case escape:
			f.state = text
			switch {
			case b == ']' || b == 'P' || b == 'X' || b == '^' || b == '_':
				f.state = str
				continue
			case b == '[' || b >= 0x20 && b <= 0x2f:
				f.state = skip
				continue
			case b >= 0x30 && b <= 0x7e:
				continue
			}
		case skip:
			if b >= 0x20 && b <= 0x3f {
				continue
			}
			f.state = text
			if b >= 0x40 && b <= 0x7e {
				continue
			}
		case str:
			switch b {
			case 0x07:
				f.state = text
			case 0x1b:
				f.state = stringEnd
			case '\n':
				// A string that does not end must not hide the subsequent lines.
				f.state = text
				dst = f.put(dst, b)
			}
			continue
		case stringEnd:
			f.state = text
			if b == '\\' {
				continue
			}
		}
		if b == 0x1b {
			dst = f.flush(dst)
			f.state = escape
			continue
		}
		if b == '\r' && f.Lines {
			dst = f.flush(dst)
			f.cr = true
			continue
		}
		dst = f.appendText(dst, b)
	}
	if f.Lines {
		start := base + bytes.LastIndexByte(dst[base:], '\n') + 1
		if len(dst)-start > maxLine {
			f.overflow = true
		}
		if !f.overflow {
			f.line = append(f.line, dst[start:]...)
			dst = dst[:start]
		}
	}
	return dst
}

// replaceLine removes the line that is not complete from dst, if it is not in the output
// yet. dst[base:] is the text after the output.
func (f *Filter) replaceLine(dst []byte, base int) []byte {
	start := base + bytes.LastIndexByte(dst[base:], '\n') + 1
	if f.overflow || len(dst)-start > maxLine {
		return dst
	}
	f.inLine = false
	f.held = 0
	return dst[:start]
}

// End appends the bytes that the filter holds at the end of the text: the line that is not
// complete, and the start of a line that is equal to the start of Mark. It ends that line,
// thus a message after the text starts on a new line.
func (f *Filter) End(dst []byte) []byte {
	dst = f.release(append(dst, f.line...))
	f.line = f.line[:0]
	f.cr = false
	if f.inLine {
		f.inLine = false
		dst = append(dst, '\n')
	}
	return dst
}

// appendText appends a byte of text to dst, if its character is permitted. The output is
// always correct UTF-8: a byte that is not a part of a correct character becomes U+FFFD.
// Such a byte can be a control character for a terminal that does not use UTF-8.
func (f *Filter) appendText(dst []byte, b byte) []byte {
	if b < utf8.RuneSelf {
		dst = f.flush(dst)
		if b == '\n' || b == '\t' || b >= 0x20 && b != 0x7f {
			dst = f.put(dst, b)
		}
		return dst
	}
	f.pend = append(f.pend, b)
	for len(f.pend) > 0 && utf8.FullRune(f.pend) {
		r, size := utf8.DecodeRune(f.pend)
		switch {
		case r == utf8.RuneError && size == 1:
			dst = f.putRune(dst, utf8.RuneError)
		case !f.inLine && unicode.In(r, unicode.Mn, unicode.Me):
			// A combining character with no character before it in the line.
		case invisible(r) && (!f.inLine || f.held > 0):
		case permitted(r):
			dst = f.putRune(dst, r)
		}
		f.pend = f.pend[:copy(f.pend, f.pend[size:])]
	}
	return dst
}

// flush ends a character that is not complete.
func (f *Filter) flush(dst []byte) []byte {
	if len(f.pend) > 0 {
		dst = f.putRune(dst, utf8.RuneError)
		f.pend = f.pend[:0]
	}
	return dst
}

// put appends one ASCII character of the output. It holds the start of a line while that
// is equal to the start of Mark, and writes a space before a full Mark.
func (f *Filter) put(dst []byte, b byte) []byte {
	if !f.inLine || f.held > 0 {
		if b == Mark[f.held] {
			f.inLine = true
			if f.held++; f.held == len(Mark) {
				f.held = 0
				dst = append(append(dst, ' '), Mark...)
			}
			return dst
		}
		dst = f.release(dst)
	}
	f.inLine = b != '\n'
	if b == '\n' {
		f.overflow = false
	}
	return append(dst, b)
}

// putRune appends one character above ASCII of the output.
func (f *Filter) putRune(dst []byte, r rune) []byte {
	f.inLine = true
	return utf8.AppendRune(f.release(dst), r)
}

// release appends the start of a line that put holds.
func (f *Filter) release(dst []byte) []byte {
	dst = append(dst, Mark[:f.held]...)
	f.held = 0
	return dst
}

// permitted is false for the characters above ASCII that change what a terminal shows: the
// C1 control characters, and the characters that change the direction of the text. With
// those, a line can show a different text than it contains.
func permitted(r rune) bool {
	return !unicode.IsControl(r) && !unicode.Is(unicode.Bidi_Control, r)
}

// invisible is true for the characters that a terminal does not show, for example a space
// of no width. With one of them before Mark or in Mark, a line of the guest looks like a
// message of the sandbox-vm process, but its bytes are not those of Mark. In other text
// they stay: some languages and emoji need them.
func invisible(r rune) bool {
	return unicode.In(r, unicode.Cf, unicode.Other_Default_Ignorable_Code_Point, unicode.Variation_Selector)
}
