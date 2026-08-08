package runtimeapi

import "unicode/utf8"

type terminalEscapeState uint8

const (
	escapeText terminalEscapeState = iota
	escapeStart
	escapeCSI
	escapeString
	escapeStringEnd
)

type terminalOutputSanitizer struct {
	state       terminalEscapeState
	csi         []byte
	utf8Pending []byte
}

func newTerminalOutputSanitizer() *terminalOutputSanitizer { return &terminalOutputSanitizer{} }

func (s *terminalOutputSanitizer) Push(data []byte) []byte {
	filtered := make([]byte, 0, len(data))
	for _, value := range data {
		switch s.state {
		case escapeText:
			if value == 0x1b {
				s.state = escapeStart
				continue
			}
			if value < 0x20 && value != '\b' && value != '\t' && value != '\n' && value != '\r' {
				continue
			}
			if value == 0x7f {
				continue
			}
			filtered = append(filtered, value)
		case escapeStart:
			switch value {
			case '[':
				s.state = escapeCSI
				s.csi = append(s.csi[:0], 0x1b, '[')
			case ']', 'P', '_', '^':
				s.state = escapeString
			default:
				s.state = escapeText
			}
		case escapeCSI:
			if len(s.csi) >= 64 {
				s.csi = s.csi[:0]
				s.state = escapeText
				continue
			}
			s.csi = append(s.csi, value)
			if value >= 0x40 && value <= 0x7e {
				if safeCSIFinal(value) {
					filtered = append(filtered, s.csi...)
				}
				s.csi = s.csi[:0]
				s.state = escapeText
			}
		case escapeString:
			if value == 0x07 {
				s.state = escapeText
			} else if value == 0x1b {
				s.state = escapeStringEnd
			}
		case escapeStringEnd:
			if value == '\\' {
				s.state = escapeText
			} else if value != 0x1b {
				s.state = escapeString
			}
		}
	}
	return s.filterUnicode(filtered, false)
}

func (s *terminalOutputSanitizer) Flush() []byte {
	s.state = escapeText
	s.csi = s.csi[:0]
	return s.filterUnicode(nil, true)
}

func safeCSIFinal(value byte) bool {
	switch value {
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'J', 'K', 'S', 'T', 'f', 'm':
		return true
	default:
		return false
	}
}

func (s *terminalOutputSanitizer) filterUnicode(data []byte, flush bool) []byte {
	buffer := append(s.utf8Pending, data...)
	s.utf8Pending = s.utf8Pending[:0]
	output := make([]byte, 0, len(buffer))
	for len(buffer) > 0 {
		if !utf8.FullRune(buffer) {
			if !flush {
				s.utf8Pending = append(s.utf8Pending, buffer...)
				break
			}
			return output
		}
		runeValue, size := utf8.DecodeRune(buffer)
		if runeValue == utf8.RuneError && size == 1 {
			buffer = buffer[1:]
			continue
		}
		if !isBidiControl(runeValue) {
			output = append(output, buffer[:size]...)
		}
		buffer = buffer[size:]
	}
	return output
}

func isBidiControl(value rune) bool {
	return value == '\u061c' || value == '\u200e' || value == '\u200f' || value >= '\u202a' && value <= '\u202e' || value >= '\u2066' && value <= '\u2069'
}
