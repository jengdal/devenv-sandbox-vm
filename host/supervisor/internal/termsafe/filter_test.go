package termsafe

import (
	"io"
	"math/rand"
	"strings"
	"testing"
	"testing/iotest"
	"unicode/utf8"
)

func TestCopy(t *testing.T) {
	in := "plain\n" +
		"<<< Welcome to NixOS >>>\r\n" +
		"[\x1b[0;32m  OK  \x1b[0m] Reached target \x1b[0;1;39mSystem Reboot\x1b[0m.\r\r\n" +
		"\r\n" +
		"progress 1\rprogress 2\n"
	want := "plain\n" +
		"<<< Welcome to NixOS >>>\n" +
		"[  OK  ] Reached target System Reboot.\n" +
		"\n" +
		"progress 1progress 2\n"
	var out strings.Builder
	Copy(&out, strings.NewReader(in))
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

// The guest must not control the terminal of the user.
func TestCopyRemovesControl(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"title", "a\x1b]0;title\x07b\n", "ab\n"},
		{"clipboard", "a\x1b]52;c;aGk=\x1b\\b\n", "ab\n"},
		{"string without an end", "a\x1b]0;title\nb\n", "a\nb\n"},
		{"cursor", "a\x1b[2J\x1b[1;1H\x1b[?25lb\n", "ab\n"},
		{"color", "\x1b[1;31mred\x1b[m\n", "red\n"},
		{"two bytes", "a\x1bcb\x1b7c\x1b(Bd\n", "abcd\n"},
		{"control characters", "a\x00\x07\x08\x0b\x7fb\tc\n", "ab\tc\n"},
		{"C1 in UTF-8", "a\u009b2Jb\u009d0;t\u009c é\n", "a2Jb0;t é\n"},
		{"C1 as one byte", "a\x9b2Jb\x9d0;t\x9c\n", "a\ufffd2Jb\ufffd0;t\ufffd\n"},
		{"escape at the end of a sequence", "\x1b[1\x1b[32mgreen\n", "green\n"},
		{"newline in a sequence", "a\x1b[1\nb\n", "a\nb\n"},
		{"byte that is not UTF-8", "a\xffb\n", "a\ufffdb\n"},
		{"character that is not complete", "a\xe2\x82\n", "a\ufffd\n"},
		{"character that is not complete before a sequence", "a\xe2\x82\x1b[mb\n", "a\ufffdb\n"},
		{"direction of the text", "a\u202eb\u2066c\u200fd\n", "abcd\n"},
		{"text above ASCII", "é – 日本 😀\n", "é – 日本 😀\n"},
		{"U+FFFD of the guest", "a\ufffdb\n", "a\ufffdb\n"},
		{"line like a message", "[sandbox-vm] ready\n[sandbox-vm]\n", " [sandbox-vm] ready\n [sandbox-vm]\n"},
		{"line like a message after a sequence", "x\r\n\x1b[1m[sand\x1b[mbox-vm] ready\n", "x\n [sandbox-vm] ready\n"},
		{"start of a message", "[sandbox\n[s\u00e9\n[sandbox-vm", "[sandbox\n[s\u00e9\n[sandbox-vm\n"},
		{"line that is not complete at the end", "Done. The new configuration is ", "Done. The new configuration is \n"},
		{"message in a line", "a [sandbox-vm] b\n", "a [sandbox-vm] b\n"},
		{"invisible character before a message", "\u200b[sandbox-vm] ready\n", " [sandbox-vm] ready\n"},
		{"invisible character in a message", "[sand\u2060box\u00ad-vm] ready\n", " [sandbox-vm] ready\n"},
		{"tag and selector before a message", "\U000e0001\ufe0f\ufeff[sandbox-vm]\n", " [sandbox-vm]\n"},
		{"combining character before a message", "\u0301\u034f[sandbox-vm]\n", " [sandbox-vm]\n"},
		{"invisible character in text", "a\u200db e\u0301 \u2764\ufe0f\n", "a\u200db e\u0301 \u2764\ufe0f\n"},
	} {
		// One byte in each read: a sequence or a character can start in one block and end in
		// the subsequent one.
		for _, r := range []io.Reader{strings.NewReader(tc.in), iotest.OneByteReader(strings.NewReader(tc.in))} {
			var out strings.Builder
			Copy(&out, r)
			if out.String() != tc.want {
				t.Errorf("%s: got %q, want %q", tc.name, out.String(), tc.want)
			}
		}
	}
}

// For the output of a command, a carriage return replaces the line.
func TestCopyLines(t *testing.T) {
	long := strings.Repeat("x", maxLine+1)
	for _, tc := range []struct{ name, in, want string }{
		{"progress", "count 1%\rcount 50%\rcount 100%, done.\nnext\n", "count 100%, done.\nnext\n"},
		{"line ends", "a\r\nb\r\r\nc\n", "a\nb\nc\n"},
		{"carriage return at the end", "a\nb\r", "a\nb\n"},
		{"sequence after a carriage return", "a\r\x1b[Kb\n", "b\n"},
		{"character that is not complete", "a\xe2\x82\rb\n", "b\n"},
		{"message after a carriage return", "x\r[sandbox-vm] ready\n", " [sandbox-vm] ready\n"},
		{"start of a message before a carriage return", "[sand\rbox-vm] ready\n", "box-vm] ready\n"},
		{"long line", long + "\ry\n", long + "y\n"},
		{"line after a long line", long + "\nx\ry\n", long + "\ny\n"},
	} {
		for _, r := range []io.Reader{strings.NewReader(tc.in), iotest.OneByteReader(strings.NewReader(tc.in))} {
			var out strings.Builder
			CopyLines(&out, r)
			if out.String() != tc.want {
				t.Errorf("%s: got %q, want %q", tc.name, out.String(), tc.want)
			}
		}
	}
}

// The protection must hold for all bytes, not only for the sequences that the filter knows.
func TestCopyRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	// These bytes start and end sequences and characters. They occur more frequently.
	special := []byte("\x1b[]P\\\x07\n\r;0m\x9b\x9d\xc2\xe2\x80\x9b\xae")
	for range 2000 {
		in := make([]byte, rng.Intn(64))
		for i := range in {
			if rng.Intn(2) == 0 {
				in[i] = special[rng.Intn(len(special))]
			} else {
				in[i] = byte(rng.Intn(256))
			}
		}
		var out strings.Builder
		Copy(&out, iotest.OneByteReader(strings.NewReader(string(in))))
		if !utf8.ValidString(out.String()) {
			t.Fatalf("%q: the output %q is not UTF-8", in, out.String())
		}
		for _, r := range out.String() {
			if r != '\n' && r != '\t' && !permitted(r) {
				t.Fatalf("%q: the output %q has %U", in, out.String(), r)
			}
		}
	}
}

// The rules of the filter, for all inputs. `go test` checks the inputs of f.Add and of
// testdata/fuzz. To look for new inputs: go test -fuzz FuzzCopy ./internal/termsafe
func FuzzCopy(f *testing.F) {
	for _, seed := range []string{
		"plain\n", "\x1b]0;title\x07", "\x1b[1;31mred\x1b[m\n", "a\x9b2J\xe2\x82",
		"[sandbox-vm] ready\n", "x\r\n\x1b[1m[sand\x1b[mbox-vm]\n", "\u200b[sand\u2060box-vm]",
		"\n[sandbox-vm", "[\n[sandbox-vm]", "\x1b\\[sandbox-vm]", "\u0301[sandbox-vm]\x1b",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		got := checkCopy(t, Copy, in)
		checkCopy(t, CopyLines, in)
		// A Writer gives the same text, without the end of a line that is not complete.
		var w strings.Builder
		if n, err := NewWriter(&w).Write(in); n != len(in) || err != nil {
			t.Fatalf("%q: Write gave %d, %v", in, n, err)
		}
		if !strings.HasPrefix(got, w.String()) {
			t.Fatalf("%q: Copy gave %q, a Writer gave %q", in, got, w.String())
		}
	})
}

// checkCopy checks the rules of the filter for one input, and returns the output.
func checkCopy(t *testing.T, copyText func(io.Writer, io.Reader), in []byte) string {
	t.Helper()
	var out strings.Builder
	copyText(&out, strings.NewReader(string(in)))
	got := out.String()
	if !utf8.ValidString(got) {
		t.Fatalf("%q: the output %q is not UTF-8", in, got)
	}
	for _, r := range got {
		if r != '\n' && r != '\t' && !permitted(r) {
			t.Fatalf("%q: the output %q has %U", in, got, r)
		}
	}
	// No line looks like a message of the sandbox-vm process, also not after the
	// characters that a terminal does not show.
	for _, line := range strings.Split(got, "\n") {
		shown := strings.Map(func(r rune) rune {
			if invisible(r) {
				return -1
			}
			return r
		}, line)
		if strings.HasPrefix(shown, Mark) {
			t.Fatalf("%q: the output has the line %q", in, line)
		}
	}
	// The blocks in which the bytes come do not change the output.
	var single strings.Builder
	copyText(&single, iotest.OneByteReader(strings.NewReader(string(in))))
	if single.String() != got {
		t.Fatalf("%q: %q in one block, %q byte by byte", in, got, single.String())
	}
	return got
}
