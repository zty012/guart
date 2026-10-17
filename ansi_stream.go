package main

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Keep a bounded, theme-independent SGR snapshot, not a replay of the entire
// stream. The Terminal component remains responsible for ANSI rendering.
type ansiStyle struct {
	fg, bg                                        string
	bold, dim, italic, underline, inverse, strike bool
}

func (s ansiStyle) prefix() string {
	var codes []string
	for _, a := range []struct {
		on   bool
		code string
	}{
		{s.bold, "1"}, {s.dim, "2"}, {s.italic, "3"},
		{s.underline, "4"}, {s.inverse, "7"}, {s.strike, "9"},
	} {
		if a.on {
			codes = append(codes, a.code)
		}
	}
	if s.fg != "" {
		codes = append(codes, s.fg)
	}
	if s.bg != "" {
		codes = append(codes, s.bg)
	}
	if len(codes) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func (s *ansiStyle) apply(params string) {
	if params == "" {
		*s = ansiStyle{}
		return
	}
	codes := strings.Split(params, ";")
	for i := 0; i < len(codes); i++ {
		n, err := strconv.Atoi(codes[i])
		if err != nil {
			return
		}
		switch {
		case n == 0:
			*s = ansiStyle{}
		case n == 1:
			s.bold = true
		case n == 2:
			s.dim = true
		case n == 3:
			s.italic = true
		case n == 4:
			s.underline = true
		case n == 7:
			s.inverse = true
		case n == 9:
			s.strike = true
		case n == 22:
			s.bold, s.dim = false, false
		case n == 23:
			s.italic = false
		case n == 24:
			s.underline = false
		case n == 27:
			s.inverse = false
		case n == 29:
			s.strike = false
		case n == 39:
			s.fg = ""
		case n == 49:
			s.bg = ""
		case n >= 30 && n <= 37 || n >= 90 && n <= 97:
			s.fg = codes[i]
		case n >= 40 && n <= 47 || n >= 100 && n <= 107:
			s.bg = codes[i]
		case n == 38 || n == 48:
			count := 0
			if i+1 < len(codes) {
				switch codes[i+1] {
				case "5":
					count = 2
				case "2":
					count = 4
				}
			}
			if count == 0 || i+count >= len(codes) {
				return
			}
			for j := i + 2; j <= i+count; j++ {
				v, err := strconv.Atoi(codes[j])
				if err != nil || v < 0 || v > 255 {
					return
				}
			}
			color := strings.Join(codes[i:i+count+1], ";")
			if n == 38 {
				s.fg = color
			} else {
				s.bg = color
			}
			i += count
		}
	}
}

const maxANSIPending = 1024

type ansiStream struct {
	style   ansiStyle
	pending string
}

// feed handles escape/UTF-8 boundaries independently of Read and bubble
// boundaries. It returns complete terminal input and searchable plain text.
// Calling it on a copy produces a preview without advancing the live stream.
func (s *ansiStream) feed(data []byte) (string, string) {
	d := s.pending + string(data)
	s.pending = ""
	var out, plain strings.Builder
	for i := 0; i < len(d); {
		if d[i] == 0x1b {
			end := i + 1
			if end < len(d) {
				switch d[end] {
				case '[':
					end++
					for end < len(d) && (d[end] < 0x40 || d[end] > 0x7e) && d[end] != 0x1b {
						end++
					}
					if end < len(d) && d[end] == 0x1b {
						i = end
						continue
					}
					if end < len(d) {
						if d[end] == 'm' {
							s.style.apply(d[i+2 : end])
						}
						out.WriteString(d[i : end+1])
						i = end + 1
						continue
					}
				case ']', 'P', '^', '_':
					kind := d[end]
					end++
					for end < len(d) {
						if kind == ']' && d[end] == '\a' {
							end++
							break
						}
						if d[end] == 0x1b && end+1 < len(d) && d[end+1] == '\\' {
							end += 2
							break
						}
						end++
					}
					if end <= len(d) && (end > i+2 && (d[end-1] == '\a' || d[end-1] == '\\' && d[end-2] == 0x1b)) {
						// Device-controlled titles/links/strings are not UI actions.
						i = end
						continue
					}
				default:
					i += 2
					continue
				}
			}
			if len(d)-i <= maxANSIPending {
				s.pending = d[i:]
				break
			}
			// Recover from an unterminated or maliciously large escape.
			out.WriteString("[ESC]")
			plain.WriteString("[ESC]")
			i++
			continue
		}
		if !utf8.FullRuneInString(d[i:]) {
			s.pending = d[i:]
			break
		}
		r, n := utf8.DecodeRuneInString(d[i:])
		i += n
		switch r {
		case '\n':
			// Serial logs commonly use LF alone; start each line at column 0.
			out.WriteString("\r\n")
			plain.WriteByte('\n')
		case '\r':
			out.WriteByte('\r')
		case '\t', '\b':
			out.WriteRune(r)
			plain.WriteRune(r)
		default:
			if r >= 0x20 && r != 0x7f {
				out.WriteRune(r)
				plain.WriteRune(r)
			}
		}
	}
	return out.String(), plain.String()
}

func (p packet) ansiText(data []byte) (input, plain string) {
	s := p.ansi
	prefix := s.style.prefix()
	input, plain = s.feed(data)
	return prefix + input, plain
}
