package usvg

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// This file ports roxmltree 0.20 (tokenizer.rs and parse.rs) with
// usvg's options (a DTD allowed, no node limit): what XML it accepts,
// the tree it builds, and the error it rejects the rest with, word for
// word. The tree keeps what usvg reads: elements with resolved
// namespaces and normalized attribute values, and each element's text
// as Node::text has it (its first child, when that is text).

const (
	nsXMLPrefix = "xml"
	nsXMLNS     = "xmlns"
	nsXMLNSURI  = "http://www.w3.org/2000/xmlns/"
)

// textPos is roxmltree's TextPos: a 1-based row and column, the column
// counted in characters.
type textPos struct{ row, col int }

func (p textPos) String() string { return fmt.Sprintf("%d:%d", p.row, p.col) }

func xmlErr(format string, args ...any) error { return fmt.Errorf(format, args...) }

var (
	errUnclosedRoot     = errors.New("the root node was opened but never closed")
	errUnexpectedEOS    = errors.New("unexpected end of stream")
	errNamespacesLimit  = errors.New("more than 2^16 unique namespaces were parsed")
	errUnreachableToken = errors.New("unreachable tokenizer state")
)

// rustChar is a byte shown as Rust's `u8 as char`.
func rustChar(b byte) string { return string(rune(b)) }

// rustCharDebug is Rust's {:?} of a char roxmltree rejects as non-XML
// (a control character or U+FFFE/U+FFFF, none of them printable).
func rustCharDebug(c rune) string {
	switch c {
	case 0:
		return `'\0'`
	case '\t':
		return `'\t'`
	case '\r':
		return `'\r'`
	case '\n':
		return `'\n'`
	}
	return fmt.Sprintf(`'\u{%x}'`, c)
}

func isXMLSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

func isXMLNameByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') ||
		b == ':' || b == '_' || b == '-' || b == '.'
}

func isXMLNameStart(c rune) bool {
	if c <= 128 {
		return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == ':' || c == '_'
	}
	return (c >= 0xC0 && c <= 0xD6) || (c >= 0xD8 && c <= 0xF6) || (c >= 0xF8 && c <= 0x2FF) ||
		(c >= 0x370 && c <= 0x37D) || (c >= 0x37F && c <= 0x1FFF) || (c >= 0x200C && c <= 0x200D) ||
		(c >= 0x2070 && c <= 0x218F) || (c >= 0x2C00 && c <= 0x2FEF) || (c >= 0x3001 && c <= 0xD7FF) ||
		(c >= 0xF900 && c <= 0xFDCF) || (c >= 0xFDF0 && c <= 0xFFFD) || (c >= 0x10000 && c <= 0xEFFFF)
}

func isXMLName(c rune) bool {
	if c <= 128 {
		return c < 128 && isXMLNameByte(byte(c))
	}
	return c == 0xB7 || (c >= 0xC0 && c <= 0xD6) || (c >= 0xD8 && c <= 0xF6) || (c >= 0xF8 && c <= 0x2FF) ||
		(c >= 0x300 && c <= 0x36F) || (c >= 0x370 && c <= 0x37D) || (c >= 0x37F && c <= 0x1FFF) ||
		(c >= 0x200C && c <= 0x200D) || (c >= 0x203F && c <= 0x2040) || (c >= 0x2070 && c <= 0x218F) ||
		(c >= 0x2C00 && c <= 0x2FEF) || (c >= 0x3001 && c <= 0xD7FF) || (c >= 0xF900 && c <= 0xFDCF) ||
		(c >= 0xFDF0 && c <= 0xFFFD) || (c >= 0x10000 && c <= 0xEFFFF)
}

func isXMLChar(c rune) bool {
	if c < 0x20 {
		return isXMLSpace(byte(c))
	}
	return c != 0xFFFF && c != 0xFFFE
}

// xmlStream is tokenizer.rs's Stream: a window [pos, end) over the
// whole document, which positions are always reported against.
type xmlStream struct {
	text     string
	pos, end int
}

func (s *xmlStream) atEnd() bool { return s.pos >= s.end }

func (s *xmlStream) currByte() (byte, error) {
	if s.atEnd() {
		return 0, errUnexpectedEOS
	}
	return s.text[s.pos], nil
}

func (s *xmlStream) nextByte() (byte, error) {
	if s.pos+1 >= s.end {
		return 0, errUnexpectedEOS
	}
	return s.text[s.pos+1], nil
}

func (s *xmlStream) advance(n int) { s.pos += n }

func (s *xmlStream) startsWith(p string) bool { return strings.HasPrefix(s.text[s.pos:s.end], p) }

func (s *xmlStream) consumeByte(c byte) error {
	curr, err := s.currByte()
	if err != nil {
		return err
	}
	if curr != c {
		return xmlErr("expected '%s' not '%s' at %s", rustChar(c), rustChar(curr), s.genTextPos())
	}
	s.advance(1)
	return nil
}

func (s *xmlStream) tryConsumeByte(c byte) bool {
	if b, err := s.currByte(); err == nil && b == c {
		s.advance(1)
		return true
	}
	return false
}

func (s *xmlStream) skipString(text string) error {
	if !s.startsWith(text) {
		return xmlErr("expected '%s' at %s", text, s.genTextPos())
	}
	s.advance(len(text))
	return nil
}

func (s *xmlStream) consumeBytes(f func(byte) bool) string {
	start := s.pos
	s.skipBytes(f)
	return s.text[start:s.pos]
}

func (s *xmlStream) skipBytes(f func(byte) bool) {
	for !s.atEnd() && f(s.text[s.pos]) {
		s.advance(1)
	}
}

// char is the character at pos and its width.
func (s *xmlStream) char() (rune, int) { return utf8.DecodeRuneInString(s.text[s.pos:s.end]) }

func (s *xmlStream) consumeChars(f func(*xmlStream, rune) bool) (string, error) {
	start := s.pos
	err := s.skipChars(f)
	return s.text[start:s.pos], err
}

func (s *xmlStream) skipChars(f func(*xmlStream, rune) bool) error {
	for !s.atEnd() {
		c, w := s.char()
		switch {
		case !isXMLChar(c):
			return xmlErr("a non-XML character %s found at %s", rustCharDebug(c), s.genTextPos())
		case f(s, c):
			s.advance(w)
		default:
			return nil
		}
	}
	return nil
}

func (s *xmlStream) skipSpaces() {
	for s.startsWithSpace() {
		s.advance(1)
	}
}

func (s *xmlStream) startsWithSpace() bool { return !s.atEnd() && isXMLSpace(s.text[s.pos]) }

func (s *xmlStream) consumeSpaces() error {
	if s.atEnd() {
		return errUnexpectedEOS
	}
	if !s.startsWithSpace() {
		return xmlErr("expected a whitespace not '%s' at %s", rustChar(s.text[s.pos]), s.genTextPos())
	}
	s.skipSpaces()
	return nil
}

// xmlRef is a Reference: a character, or the name of an entity.
type xmlRef struct {
	char   rune
	entity string
}

func (s *xmlStream) tryConsumeReference() (xmlRef, bool) {
	sub := *s
	ref, ok := sub.consumeReference()
	if ok {
		s.pos = sub.pos
	}
	return ref, ok
}

func (s *xmlStream) consumeReference() (xmlRef, bool) {
	if !s.tryConsumeByte('&') {
		return xmlRef{}, false
	}
	var ref xmlRef
	if s.tryConsumeByte('#') {
		var value string
		radix := 10
		if s.tryConsumeByte('x') {
			value = s.consumeBytes(func(c byte) bool {
				return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')
			})
			radix = 16
		} else {
			value = s.consumeBytes(func(c byte) bool { return c >= '0' && c <= '9' })
		}
		n, err := strconv.ParseUint(value, radix, 32)
		if err != nil {
			return xmlRef{}, false
		}
		c := rune(n)
		if n > utf8.MaxRune || (n >= 0xD800 && n <= 0xDFFF) {
			c = utf8.RuneError // char::from_u32's None
		}
		if !isXMLChar(c) {
			return xmlRef{}, false
		}
		ref.char = c
	} else {
		name, err := s.consumeName()
		if err != nil {
			return xmlRef{}, false
		}
		switch name {
		case "quot":
			ref.char = '"'
		case "amp":
			ref.char = '&'
		case "apos":
			ref.char = '\''
		case "lt":
			ref.char = '<'
		case "gt":
			ref.char = '>'
		default:
			ref.entity = name
		}
	}
	if s.consumeByte(';') != nil {
		return xmlRef{}, false
	}
	return ref, true
}

func (s *xmlStream) consumeName() (string, error) {
	start := s.pos
	if err := s.skipName(); err != nil {
		return "", err
	}
	if s.pos == start {
		return "", xmlErr("invalid name token at %s", s.genTextPosFrom(start))
	}
	return s.text[start:s.pos], nil
}

func (s *xmlStream) skipName() error {
	start := s.pos
	if s.atEnd() {
		return nil
	}
	c, w := s.char()
	if !isXMLNameStart(c) {
		return xmlErr("invalid name token at %s", s.genTextPosFrom(start))
	}
	s.advance(w)
	for !s.atEnd() {
		c, w := s.char()
		if !isXMLName(c) {
			break
		}
		s.advance(w)
	}
	return nil
}

func (s *xmlStream) consumeQName() (prefix, local string, err error) {
	start := s.pos
	splitter := -1
	for !s.atEnd() {
		b := s.text[s.pos]
		if b < 128 {
			if b == ':' {
				if splitter >= 0 {
					return "", "", xmlErr("invalid name token at %s", s.genTextPosFrom(start))
				}
				splitter = s.pos
				s.advance(1)
			} else if isXMLNameByte(b) {
				s.advance(1)
			} else {
				break
			}
		} else {
			c, w := s.char()
			if !isXMLName(c) {
				break
			}
			s.advance(w)
		}
	}
	if splitter >= 0 {
		prefix, local = s.text[start:splitter], s.text[splitter+1:s.pos]
	} else {
		local = s.text[start:s.pos]
	}
	if prefix != "" {
		if c, _ := utf8.DecodeRuneInString(prefix); !isXMLNameStart(c) {
			return "", "", xmlErr("invalid name token at %s", s.genTextPosFrom(start))
		}
	}
	if c, _ := utf8.DecodeRuneInString(local); local == "" || !isXMLNameStart(c) {
		return "", "", xmlErr("invalid name token at %s", s.genTextPosFrom(start))
	}
	return prefix, local, nil
}

func (s *xmlStream) consumeEq() error {
	s.skipSpaces()
	if err := s.consumeByte('='); err != nil {
		return err
	}
	s.skipSpaces()
	return nil
}

func (s *xmlStream) consumeQuote() (byte, error) {
	c, err := s.currByte()
	if err != nil {
		return 0, err
	}
	if c != '\'' && c != '"' {
		return 0, xmlErr("expected a quote not '%s' at %s", rustChar(c), s.genTextPos())
	}
	s.advance(1)
	return c, nil
}

func (s *xmlStream) genTextPos() textPos { return textPosAt(s.text, s.pos) }

func (s *xmlStream) genTextPosFrom(pos int) textPos { return textPosAt(s.text, min(pos, len(s.text))) }

// textPosAt is gen_text_pos: rows by newline bytes, the column by
// characters back to the last newline.
func textPosAt(text string, end int) textPos {
	pos := textPos{row: 1 + strings.Count(text[:end], "\n"), col: 1}
	line := text[:end]
	if i := strings.LastIndexByte(line, '\n'); i >= 0 {
		line = line[i+1:]
	}
	pos.col += utf8.RuneCountInString(line)
	return pos
}

// span is a StrSpan: a range of the document.
type span struct{ start, end int }

// nsBinding is a namespace in scope: a prefix (none for the default
// namespace) and its URI.
type nsBinding struct {
	prefix    string
	hasPrefix bool
	uri       string
}

type xmlEntity struct {
	name  string
	value span
}

type tagNameSpan struct {
	prefix, name   string
	pos, prefixPos int
}

type tempAttr struct {
	prefix, local, value string
	start                int
}

// loopDetector is roxmltree's entity loop guard: 10 levels deep, 255
// references under one root reference.
type loopDetector struct{ depth, references uint8 }

func (l *loopDetector) incDepth(s *xmlStream) error {
	if l.depth < 10 {
		l.depth++
		return nil
	}
	return xmlErr("a possible entity reference loop is detected at %s", s.genTextPos())
}

func (l *loopDetector) decDepth() {
	if l.depth > 0 {
		l.depth--
	}
	if l.depth == 0 {
		l.references = 0
	}
}

func (l *loopDetector) incReferences(s *xmlStream) error {
	if l.depth == 0 {
		return nil
	}
	if l.references == 255 {
		return xmlErr("a possible entity reference loop is detected at %s", s.genTextPos())
	}
	l.references++
	return nil
}

// xmlParser is parse.rs's Context with the tokenizer's events
// delivered directly.
type xmlParser struct {
	text           string
	root           *xmlNode
	parent         *xmlNode
	parentPrefixes []string
	ownNS          []nsBinding // the current element's own declarations
	uniqueNS       map[nsBinding]bool
	attrs          []tempAttr
	entities       []xmlEntity
	afterText      bool
	// lastText is where merged text goes: the element whose first child
	// is the text node just appended, else nil.
	lastText *xmlNode
	tagName  tagNameSpan
	loops    loopDetector
}

// parseXML is Document::parse_with_options with allow_dtd.
func parseXML(text string) (*xmlNode, error) {
	p := &xmlParser{
		text:           text,
		root:           &xmlNode{},
		parentPrefixes: []string{""},
		uniqueNS:       map[nsBinding]bool{{prefix: nsXMLPrefix, hasPrefix: true, uri: xmlNS}: true},
	}
	p.parent = p.root
	if err := p.parseDocument(&xmlStream{text: text, end: len(text)}); err != nil {
		return nil, err
	}
	if len(p.root.children) == 0 {
		return nil, errNoRoot
	}
	if len(p.parentPrefixes) > 1 {
		return nil, errUnclosedRoot
	}
	return p.root, nil
}

func (p *xmlParser) errPosAt(pos int) textPos { return textPosAt(p.text, min(pos, len(p.text))) }

// document ::= prolog element Misc*
func (p *xmlParser) parseDocument(s *xmlStream) error {
	if s.startsWith("\xEF\xBB\xBF") {
		s.advance(3)
	}
	if s.startsWith("<?xml ") {
		if err := parseDeclaration(s); err != nil {
			return err
		}
	}
	if err := p.parseMisc(s); err != nil {
		return err
	}
	s.skipSpaces()
	if s.startsWith("<!DOCTYPE") {
		if err := p.parseDoctype(s); err != nil {
			return err
		}
		if err := p.parseMisc(s); err != nil {
			return err
		}
	}
	s.skipSpaces()
	if b, err := s.currByte(); err == nil && b == '<' {
		if err := p.parseElement(s); err != nil {
			return err
		}
	}
	if err := p.parseMisc(s); err != nil {
		return err
	}
	if !s.atEnd() {
		return xmlErr("unknown token at %s", s.genTextPos())
	}
	return nil
}

// Misc ::= Comment | PI | S
func (p *xmlParser) parseMisc(s *xmlStream) error {
	for !s.atEnd() {
		s.skipSpaces()
		var err error
		switch {
		case s.startsWith("<!--"):
			err = p.parseComment(s)
		case s.startsWith("<?"):
			err = p.parsePI(s)
		default:
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// parseDeclaration validates the XML declaration; it yields nothing.
func parseDeclaration(s *xmlStream) error {
	spaces := func() error {
		if s.startsWithSpace() {
			s.skipSpaces()
		} else if !s.startsWith("?>") && !s.atEnd() {
			return xmlErr("expected a whitespace not '%s' at %s", rustChar(s.text[s.pos]), s.genTextPos())
		}
		return nil
	}
	s.advance(5) // <?xml
	if err := spaces(); err != nil {
		return err
	}
	if !s.startsWith("version") {
		return s.skipString("version")
	}
	if err := parseDeclAttribute(s); err != nil {
		return err
	}
	if err := spaces(); err != nil {
		return err
	}
	if s.startsWith("encoding") {
		if err := parseDeclAttribute(s); err != nil {
			return err
		}
		if err := spaces(); err != nil {
			return err
		}
	}
	if s.startsWith("standalone") {
		if err := parseDeclAttribute(s); err != nil {
			return err
		}
	}
	s.skipSpaces()
	return s.skipString("?>")
}

// parseDeclAttribute is parse_attribute (Name Eq AttValue), its value
// unused: the declaration is only validated.
func parseDeclAttribute(s *xmlStream) error {
	if _, _, err := s.consumeQName(); err != nil {
		return err
	}
	if err := s.consumeEq(); err != nil {
		return err
	}
	_, err := attrValue(s)
	return err
}

// attrValue is a quoted value without '<', the quotes consumed.
func attrValue(s *xmlStream) (span, error) {
	quote, err := s.consumeQuote()
	if err != nil {
		return span{}, err
	}
	start := s.pos
	q := rune(quote)
	if err := s.skipChars(func(_ *xmlStream, c rune) bool { return c != q && c != '<' }); err != nil {
		return span{}, err
	}
	value := span{start, s.pos}
	return value, s.consumeByte(quote)
}

// '<!--' ((Char - '-') | ('-' (Char - '-')))* '-->'
func (p *xmlParser) parseComment(s *xmlStream) error {
	start := s.pos
	s.advance(4)
	text, err := s.consumeChars(func(s *xmlStream, c rune) bool { return c != '-' || !s.startsWith("-->") })
	if err != nil {
		return err
	}
	if err := s.skipString("-->"); err != nil {
		return err
	}
	if strings.Contains(text, "--") || strings.HasSuffix(text, "-") {
		return xmlErr("comment at %s contains '--'", s.genTextPosFrom(start))
	}
	p.appendNode()
	p.afterText = false
	return nil
}

// PI ::= '<?' PITarget (S (Char* - (Char* '?>' Char*)))? '?>'
func (p *xmlParser) parsePI(s *xmlStream) error {
	if s.startsWith("<?xml ") {
		return xmlErr("unexpected XML declaration at %s", s.genTextPos())
	}
	s.advance(2)
	if _, err := s.consumeName(); err != nil {
		return err
	}
	s.skipSpaces()
	if _, err := s.consumeChars(func(s *xmlStream, c rune) bool { return c != '?' || !s.startsWith("?>") }); err != nil {
		return err
	}
	if err := s.skipString("?>"); err != nil {
		return err
	}
	p.appendNode()
	p.afterText = false
	return nil
}

func (p *xmlParser) parseDoctype(s *xmlStream) error {
	start := s.pos
	if err := parseDoctypeStart(s); err != nil {
		return err
	}
	s.skipSpaces()
	if b, err := s.currByte(); err == nil && b == '>' {
		s.advance(1)
		return nil
	}
	s.advance(1) // [
	for !s.atEnd() {
		s.skipSpaces()
		var err error
		switch {
		case s.startsWith("<!ENTITY"):
			err = p.parseEntityDecl(s)
		case s.startsWith("<!--"):
			err = p.parseComment(s)
		case s.startsWith("<?"):
			err = p.parsePI(s)
		case s.startsWith("]"):
			s.advance(1)
			s.skipSpaces()
			c, cerr := s.currByte()
			if cerr != nil {
				return errUnexpectedEOS
			}
			if c != '>' {
				return xmlErr("expected '>' not '%s' at %s", rustChar(c), s.genTextPos())
			}
			s.advance(1)
			return nil
		case s.startsWith("<!ELEMENT"), s.startsWith("<!ATTLIST"), s.startsWith("<!NOTATION"):
			s.skipBytes(func(c byte) bool { return c != '>' })
			if s.consumeByte('>') != nil {
				return xmlErr("unknown token at %s", s.genTextPosFrom(start))
			}
		default:
			return xmlErr("unknown token at %s", s.genTextPos())
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// doctypedecl ::= '<!DOCTYPE' S Name (S ExternalID)? S? ('[' intSubset ']' S?)? '>'
func parseDoctypeStart(s *xmlStream) error {
	s.advance(9)
	if err := s.consumeSpaces(); err != nil {
		return err
	}
	if err := s.skipName(); err != nil {
		return err
	}
	s.skipSpaces()
	if _, err := parseExternalID(s); err != nil {
		return err
	}
	s.skipSpaces()
	c, err := s.currByte()
	if err != nil {
		return err
	}
	if c != '[' && c != '>' {
		return xmlErr("expected '[' or '>' not '%s' at %s", rustChar(c), s.genTextPos())
	}
	return nil
}

// ExternalID ::= 'SYSTEM' S SystemLiteral | 'PUBLIC' S PubidLiteral S SystemLiteral
func parseExternalID(s *xmlStream) (bool, error) {
	if !s.startsWith("SYSTEM") && !s.startsWith("PUBLIC") {
		return false, nil
	}
	public := s.startsWith("PUBLIC")
	s.advance(6)
	literals := 1
	if public {
		literals = 2
	}
	for range literals {
		if err := s.consumeSpaces(); err != nil {
			return false, err
		}
		quote, err := s.consumeQuote()
		if err != nil {
			return false, err
		}
		s.skipBytes(func(c byte) bool { return c != quote })
		if err := s.consumeByte(quote); err != nil {
			return false, err
		}
	}
	return true, nil
}

// EntityDecl ::= '<!ENTITY' S ('%' S)? Name S EntityDef S? '>'
func (p *xmlParser) parseEntityDecl(s *xmlStream) error {
	s.advance(8)
	if err := s.consumeSpaces(); err != nil {
		return err
	}
	general := true
	if s.tryConsumeByte('%') {
		if err := s.consumeSpaces(); err != nil {
			return err
		}
		general = false
	}
	name, err := s.consumeName()
	if err != nil {
		return err
	}
	if err := s.consumeSpaces(); err != nil {
		return err
	}
	value, ok, err := parseEntityDef(s, general)
	if err != nil {
		return err
	}
	if ok {
		p.entities = append(p.entities, xmlEntity{name: name, value: value})
	}
	s.skipSpaces()
	return s.consumeByte('>')
}

// parseEntityDef is an EntityValue, or an ExternalID (with NDATA for a
// general entity), which defines nothing usable.
func parseEntityDef(s *xmlStream, general bool) (span, bool, error) {
	c, err := s.currByte()
	if err != nil {
		return span{}, false, err
	}
	switch c {
	case '"', '\'':
		quote, _ := s.consumeQuote()
		start := s.pos
		s.skipBytes(func(c byte) bool { return c != quote })
		value := span{start, s.pos}
		return value, true, s.consumeByte(quote)
	case 'S', 'P':
		ok, err := parseExternalID(s)
		if err != nil {
			return span{}, false, err
		}
		if !ok {
			return span{}, false, xmlErr("invalid ExternalID at %s", s.genTextPos())
		}
		if general {
			s.skipSpaces()
			if s.startsWith("NDATA") {
				s.advance(5)
				if err := s.consumeSpaces(); err != nil {
					return span{}, false, err
				}
				if err := s.skipName(); err != nil {
					return span{}, false, err
				}
			}
		}
		return span{}, false, nil
	}
	return span{}, false, xmlErr("expected a quote, SYSTEM or PUBLIC not '%s' at %s", rustChar(c), s.genTextPos())
}

// element ::= EmptyElemTag | STag content ETag
func (p *xmlParser) parseElement(s *xmlStream) error {
	start := s.pos
	s.advance(1) // <
	prefix, local, err := s.consumeQName()
	if err != nil {
		return err
	}
	if prefix == nsXMLNS {
		return xmlErr("the 'xmlns' prefix is used at %s, but it must not be", p.errPosAt(start+1))
	}
	p.tagName = tagNameSpan{prefix: prefix, name: local, pos: start, prefixPos: start + 1}
	p.afterText = false

	open := false
loop:
	for !s.atEnd() {
		hasSpace := s.startsWithSpace()
		s.skipSpaces()
		start := s.pos
		c, err := s.currByte()
		if err != nil {
			return err
		}
		switch c {
		case '/':
			s.advance(1)
			if err := s.consumeByte('>'); err != nil {
				return err
			}
			if err := p.elementEnd(endEmpty, "", "", start); err != nil {
				return err
			}
			break loop
		case '>':
			s.advance(1)
			if err := p.elementEnd(endOpen, "", "", start); err != nil {
				return err
			}
			open = true
			break loop
		default:
			if !hasSpace {
				if err := s.consumeSpaces(); err != nil {
					return err
				}
			}
			prefix, local, err := s.consumeQName()
			if err != nil {
				return err
			}
			if err := s.consumeEq(); err != nil {
				return err
			}
			value, err := attrValue(s)
			if err != nil {
				return err
			}
			if err := p.attribute(prefix, local, value, start); err != nil {
				return err
			}
		}
	}
	if open {
		return p.parseContent(s)
	}
	return nil
}

// content ::= CharData? ((element | Reference | CDSect | PI | Comment) CharData?)*
func (p *xmlParser) parseContent(s *xmlStream) error {
	for !s.atEnd() {
		if s.text[s.pos] != '<' {
			if err := p.parseText(s); err != nil {
				return err
			}
			continue
		}
		next, err := s.nextByte()
		if err != nil {
			return xmlErr("unknown token at %s", s.genTextPos())
		}
		switch next {
		case '!':
			switch {
			case s.startsWith("<!--"):
				err = p.parseComment(s)
			case s.startsWith("<![CDATA["):
				err = p.parseCDATA(s)
			default:
				return xmlErr("unknown token at %s", s.genTextPos())
			}
		case '?':
			err = p.parsePI(s)
		case '/':
			return p.parseCloseElement(s)
		default:
			err = p.parseElement(s)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// CDSect ::= '<![CDATA[' (Char* - (Char* ']]>' Char*)) ']]>'
func (p *xmlParser) parseCDATA(s *xmlStream) error {
	s.advance(9)
	text, err := s.consumeChars(func(s *xmlStream, c rune) bool { return c != ']' || !s.startsWith("]]>") })
	if err != nil {
		return err
	}
	if err := s.skipString("]]>"); err != nil {
		return err
	}
	p.cdata(text)
	return nil
}

// '</' Name S? '>'
func (p *xmlParser) parseCloseElement(s *xmlStream) error {
	start := s.pos
	s.advance(2)
	prefix, local, err := s.consumeQName()
	if err != nil {
		return err
	}
	s.skipSpaces()
	if err := s.consumeByte('>'); err != nil {
		return err
	}
	return p.elementEnd(endClose, prefix, local, start)
}

func (p *xmlParser) parseText(s *xmlStream) error {
	start := s.pos
	text, err := s.consumeChars(func(_ *xmlStream, c rune) bool { return c != '<' })
	if err != nil {
		return err
	}
	if strings.Contains(text, ">") && strings.Contains(text, "]]>") {
		return xmlErr("']]>' at %s is not allowed inside a character data", s.genTextPos())
	}
	return p.textToken(text, span{start, s.pos})
}

// appendNode counts a child of the current parent that is not an
// element or text (a comment, a PI), which ends any text run.
func (p *xmlParser) appendNode() {
	p.parent.nkids++
	p.lastText = nil
}

// attribute is process_attribute: namespace declarations are checked
// and recorded, the rest held until the start tag ends.
func (p *xmlParser) attribute(prefix, local string, raw span, start int) error {
	value, err := p.normalizeAttribute(raw)
	if err != nil {
		return err
	}
	switch {
	case prefix == nsXMLNS:
		if value == nsXMLNSURI {
			return xmlErr("the 'xmlns' URI is used at %s, but it must not be declared", p.errPosAt(start))
		}
		isXMLURI := value == xmlNS
		if local == nsXMLPrefix {
			if !isXMLURI {
				return xmlErr("'xml' namespace prefix mapped to wrong URI at %s", p.errPosAt(start))
			}
		} else if isXMLURI {
			return xmlErr("the 'xml' namespace URI is used for not 'xml' prefix at %s", p.errPosAt(start))
		}
		for _, ns := range p.ownNS {
			if ns.hasPrefix && ns.prefix == local {
				return xmlErr("namespace '%s' at %s is already defined", local, p.errPosAt(start))
			}
		}
		if !isXMLURI {
			return p.pushNS(nsBinding{prefix: local, hasPrefix: true, uri: value})
		}
	case local == nsXMLNS:
		if value == xmlNS {
			return xmlErr("the 'xml' namespace URI is used for not 'xml' prefix at %s", p.errPosAt(start))
		}
		if value == nsXMLNSURI {
			return xmlErr("the 'xmlns' URI is used at %s, but it must not be declared", p.errPosAt(start))
		}
		return p.pushNS(nsBinding{uri: value})
	default:
		p.attrs = append(p.attrs, tempAttr{prefix: prefix, local: local, value: value, start: start})
	}
	return nil
}

// pushNS is Namespaces::push_ns: at most 2^16 distinct namespaces.
func (p *xmlParser) pushNS(ns nsBinding) error {
	if !p.uniqueNS[ns] {
		if len(p.uniqueNS) > 65535 {
			return errNamespacesLimit
		}
		p.uniqueNS[ns] = true
	}
	p.ownNS = append(p.ownNS, ns)
	return nil
}

type elementEndKind uint8

const (
	endOpen elementEndKind = iota
	endEmpty
	endClose
)

// elementEnd is process_element.
func (p *xmlParser) elementEnd(kind elementEndKind, prefix, local string, tokenStart int) error {
	if p.tagName.name == "" {
		// An entity's text closing a tag it did not open:
		// <!ENTITY p '</p>'> ... <root>&p;</root>
		if kind == endClose {
			return xmlErr("unexpected close tag at %s", p.errPosAt(tokenStart))
		}
		return errUnreachableToken
	}
	namespaces := p.resolveNamespaces()
	p.ownNS = nil
	attrs, err := p.resolveAttributes(namespaces)
	if err != nil {
		return err
	}
	switch kind {
	case endEmpty, endOpen:
		space, bound, err := p.namespaceFor(namespaces, p.tagName.prefixPos, p.tagName.prefix)
		if err != nil {
			return err
		}
		el := &xmlNode{space: space, bound: bound, name: p.tagName.name, attrs: attrs, parent: p.parent, index: len(p.parent.children), ns: namespaces}
		p.parent.children = append(p.parent.children, el)
		p.parent.nkids++
		p.lastText = nil
		if kind == endOpen {
			p.parent = el
			p.parentPrefixes = append(p.parentPrefixes, p.tagName.prefix)
		}
	case endClose:
		parentPrefix := p.parentPrefixes[len(p.parentPrefixes)-1]
		if p.parent != p.root && (prefix != parentPrefix || local != p.parent.name) {
			return xmlErr("expected '%s' tag, not '%s' at %s",
				qname(parentPrefix, p.parent.name), qname(prefix, local), p.errPosAt(tokenStart))
		}
		if p.parent == p.root {
			return errUnreachableToken
		}
		p.parent = p.parent.parent
		p.parentPrefixes = p.parentPrefixes[:len(p.parentPrefixes)-1]
	}
	p.afterText = false
	return nil
}

func qname(prefix, local string) string {
	if prefix == "" {
		return local
	}
	return prefix + ":" + local
}

// resolveNamespaces is the element's namespaces in scope: its own
// declarations, then the parent's it does not redeclare.
func (p *xmlParser) resolveNamespaces() []nsBinding {
	if p.parent == p.root {
		return p.ownNS
	}
	if len(p.ownNS) == 0 {
		return p.parent.ns
	}
	scope := p.ownNS
	for _, ns := range p.parent.ns {
		redeclared := false
		for _, own := range p.ownNS {
			if own.hasPrefix == ns.hasPrefix && own.prefix == ns.prefix {
				redeclared = true
				break
			}
		}
		if !redeclared {
			scope = append(scope, ns)
		}
	}
	return scope
}

// namespaceFor is get_ns_idx_by_prefix: the URI an element or attribute
// prefix maps to ("" for none); an unbound prefix is an error.
func (p *xmlParser) namespaceFor(scope []nsBinding, pos int, prefix string) (uri string, bound bool, err error) {
	for _, ns := range scope {
		if ns.hasPrefix == (prefix != "") && ns.prefix == prefix {
			return ns.uri, true, nil
		}
	}
	if prefix != "" {
		return "", false, xmlErr("an unknown namespace prefix '%s' at %s", prefix, p.errPosAt(pos))
	}
	return "", false, nil
}

func (p *xmlParser) resolveAttributes(scope []nsBinding) ([]xmlAttr, error) {
	pending := p.attrs
	p.attrs = nil
	var out []xmlAttr
	for _, a := range pending {
		var space string
		var bound bool
		switch a.prefix {
		case nsXMLPrefix:
			space, bound = xmlNS, true
		case "":
		default:
			var err error
			if space, bound, err = p.namespaceFor(scope, a.start, a.prefix); err != nil {
				return nil, err
			}
		}
		for _, prev := range out {
			if prev.bound == bound && prev.space == space && prev.name == a.local {
				return nil, xmlErr("attribute '%s' at %s is already defined", a.local, p.errPosAt(a.start))
			}
		}
		out = append(out, xmlAttr{space: space, bound: bound, name: a.local, value: a.value})
	}
	return out, nil
}

// textToken is process_text: character and entity references resolved,
// an entity's markup parsed in place.
func (p *xmlParser) textToken(text string, rng span) error {
	if !strings.ContainsAny(text, "&\r") {
		p.appendText(text)
		p.afterText = true
		return nil
	}
	var buf textBuffer
	asIs := false
	s := &xmlStream{text: p.text, pos: rng.start, end: rng.end}
	for !s.atEnd() {
		c := s.text[s.pos]
		if c != '&' {
			s.advance(1)
			if asIs {
				buf.pushRaw(c)
				asIs = false
			} else {
				buf.pushFromText(c, s.atEnd())
			}
			continue
		}
		start := s.pos
		ref, ok := s.tryConsumeReference()
		switch {
		case !ok:
			return xmlErr("malformed entity reference at %s", s.genTextPosFrom(start))
		case ref.entity == "":
			for _, b := range []byte(string(ref.char)) {
				if p.loops.depth > 0 {
					buf.pushFromText(b, s.atEnd())
				} else {
					// Characters not from an entity are kept as they are
					// (lxml does the same).
					buf.pushRaw(b)
					asIs = true
				}
			}
		default:
			entity, found := p.entity(ref.entity)
			if !found {
				return xmlErr("unknown entity reference '%s' at %s", ref.entity, s.genTextPosFrom(start))
			}
			asIs = false
			if len(buf) > 0 {
				p.appendText(string(buf))
				buf = buf[:0]
				p.afterText = true
			}
			if err := p.loops.incReferences(s); err != nil {
				return err
			}
			if err := p.loops.incDepth(s); err != nil {
				return err
			}
			prevTag := p.tagName
			p.tagName = tagNameSpan{}
			err := p.parseContent(&xmlStream{text: p.text, pos: entity.start, end: entity.end})
			p.tagName = prevTag
			if err != nil {
				return err
			}
			buf = buf[:0]
			p.loops.decDepth()
		}
	}
	if len(buf) > 0 {
		p.appendText(string(buf))
		p.afterText = true
	}
	return nil
}

func (p *xmlParser) entity(name string) (span, bool) {
	for _, e := range p.entities {
		if e.name == name {
			return e.value, true
		}
	}
	return span{}, false
}

// cdata is process_cdata: kept as is but for line endings.
func (p *xmlParser) cdata(text string) {
	if !strings.Contains(text, "\r") {
		p.appendText(text)
		p.afterText = true
		return
	}
	var buf textBuffer
	count := utf8.RuneCountInString(text)
	i := 0
	for _, c := range text {
		i++
		for _, b := range []byte(string(c)) {
			buf.pushFromText(b, i == count)
		}
	}
	if len(buf) > 0 {
		p.appendText(string(buf))
		p.afterText = true
	}
}

// appendText is append_text: text right after text joins it, else it is
// a new child of the current parent.
func (p *xmlParser) appendText(text string) {
	if p.afterText {
		if p.lastText != nil {
			p.lastText.text.WriteString(text)
		}
		return
	}
	p.parent.nkids++
	p.lastText = nil
	if p.parent.nkids == 1 && p.parent != p.root {
		p.parent.text.WriteString(text)
		p.lastText = p.parent
	}
}

// normalizeAttribute is AVNormalize with references resolved.
func (p *xmlParser) normalizeAttribute(value span) (string, error) {
	raw := p.text[value.start:value.end]
	if !strings.ContainsAny(raw, "&\t\n\r") {
		return raw, nil
	}
	var buf textBuffer
	if err := p.normalizeInto(value, &buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func (p *xmlParser) normalizeInto(value span, buf *textBuffer) error {
	s := &xmlStream{text: p.text, pos: value.start, end: value.end}
	for !s.atEnd() {
		c := s.text[s.pos]
		if c != '&' {
			s.advance(1)
			next, err := s.currByte()
			buf.pushFromAttr(c, next, err == nil)
			continue
		}
		start := s.pos
		ref, ok := s.tryConsumeReference()
		switch {
		case !ok:
			return xmlErr("malformed entity reference at %s", s.genTextPosFrom(start))
		case ref.entity == "":
			for _, b := range []byte(string(ref.char)) {
				if p.loops.depth > 0 {
					// An escaped '<' is fine, but not from an entity.
					if b == '<' {
						return xmlErr("unescaped '<' found at %s", s.genTextPosFrom(start))
					}
					buf.pushFromAttr(b, 0, false)
				} else {
					buf.pushRaw(b)
				}
			}
		default:
			entity, found := p.entity(ref.entity)
			if !found {
				return xmlErr("unknown entity reference '%s' at %s", ref.entity, s.genTextPosFrom(start))
			}
			if err := p.loops.incReferences(s); err != nil {
				return err
			}
			if err := p.loops.incDepth(s); err != nil {
				return err
			}
			if err := p.normalizeInto(entity, buf); err != nil {
				return err
			}
			p.loops.decDepth()
		}
	}
	return nil
}

// textBuffer is parse.rs's TextBuffer.
type textBuffer []byte

func (b *textBuffer) pushRaw(c byte) { *b = append(*b, c) }

// pushFromAttr drops the \r of \r\n and turns \n, \r, \t into spaces.
func (b *textBuffer) pushFromAttr(c, next byte, hasNext bool) {
	if c == '\r' && hasNext && next == '\n' {
		return
	}
	if c == '\n' || c == '\r' || c == '\t' {
		c = ' '
	}
	*b = append(*b, c)
}

// pushFromText turns \r\n and a lone \r into \n.
func (b *textBuffer) pushFromText(c byte, atEnd bool) {
	buf := *b
	switch {
	case len(buf) > 0 && buf[len(buf)-1] == '\r':
		buf[len(buf)-1] = '\n'
		if atEnd && c == '\r' {
			buf = append(buf, '\n')
		} else if c != '\n' {
			buf = append(buf, c)
		}
	case atEnd && c == '\r':
		buf = append(buf, '\n')
	default:
		buf = append(buf, c)
	}
	*b = buf
}
