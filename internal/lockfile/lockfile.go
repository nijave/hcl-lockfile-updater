package lockfile

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// Format selects how written bytes are formatted.
type Format int

const (
	// FormatBlock formats only the provider block the tool writes; the rest
	// of the file keeps its original bytes.
	FormatBlock Format = iota
	// FormatOff writes new bytes without a formatter pass. Set attributes
	// are spliced in plainly; everything else is byte-preserved.
	FormatOff
	// FormatFile formats the entire file (hclwrite round-trip).
	FormatFile
)

// ProviderAttrs holds the attributes the tool may set on a provider block.
// A zero-valued Constraints or empty Hashes slice means "do not touch".
type ProviderAttrs struct {
	Version     string
	Constraints string
	Hashes      []string
}

func applyAttrs(body *hclwrite.Body, attrs ProviderAttrs) {
	if attrs.Version != "" {
		body.SetAttributeValue("version", cty.StringVal(attrs.Version))
	}
	if attrs.Constraints != "" {
		body.SetAttributeValue("constraints", cty.StringVal(attrs.Constraints))
	}
	if len(attrs.Hashes) > 0 {
		vals := make([]cty.Value, 0, len(attrs.Hashes))
		for _, h := range attrs.Hashes {
			vals = append(vals, cty.StringVal(h))
		}
		body.SetAttributeRaw("hashes", hclwrite.TokensForValue(cty.ListVal(vals)))
	}
}

// RenderProviderBlock renders a single standalone provider {} block.
func RenderProviderBlock(addr string, attrs ProviderAttrs, format Format) []byte {
	if format == FormatOff {
		return []byte(renderBlockPlain(addr, attrs))
	}
	f := hclwrite.NewEmptyFile()
	b := f.Body().AppendNewBlock("provider", []string{addr})
	applyAttrs(b.Body(), attrs)
	return f.Bytes()
}

// MergeProviderBlock merges attrs into the provider block for addr within data.
// Attributes not present in attrs are preserved. If no matching block exists, a
// new one is appended. Empty data starts a fresh file.
func MergeProviderBlock(data []byte, addr string, attrs ProviderAttrs, format Format) ([]byte, error) {
	switch format {
	case FormatFile:
		return mergeWholeFile(data, addr, attrs)
	case FormatOff:
		return mergePlain(data, addr, attrs)
	default: // FormatBlock
		out, err := mergePlain(data, addr, attrs)
		if err != nil {
			return nil, err
		}
		return formatBlockOnly(out, addr)
	}
}

// mergeWholeFile updates the block and returns a formatter-normalized file.
func mergeWholeFile(data []byte, addr string, attrs ProviderAttrs) ([]byte, error) {
	f, diags := hclwrite.ParseConfig(data, ".terraform.lock.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	block := findProviderBlock(f.Body(), addr)
	if block == nil {
		block = f.Body().AppendNewBlock("provider", []string{addr})
	}
	applyAttrs(block.Body(), attrs)
	out := f.Bytes()
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out, nil
}

// mergePlain splices updated attribute expressions into the original bytes.
// Everything the tool does not set keeps its exact bytes.
func mergePlain(data []byte, addr string, attrs ProviderAttrs) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		out := renderBlockPlain(addr, attrs)
		return []byte(out), nil
	}

	f, diags := hclsyntax.ParseConfig(data, ".terraform.lock.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("unexpected body type %T", f.Body)
	}

	blk := findSyntaxProviderBlock(body, addr)
	if blk == nil {
		// No matching block: append one after the existing content.
		out := make([]byte, 0, len(data)+128)
		out = append(out, data...)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		out = append(out, renderBlockPlain(addr, attrs)...)
		return out, nil
	}

	set := map[string]string{}
	if attrs.Version != "" {
		set["version"] = plainString(attrs.Version)
	}
	if attrs.Constraints != "" {
		set["constraints"] = plainString(attrs.Constraints)
	}
	if len(attrs.Hashes) > 0 {
		set["hashes"] = plainHashes(attrs.Hashes)
	}

	// Splice expressions for attributes that already exist. Splices shift
	// byte offsets, so apply them right-to-left (descending start offset).
	type pending struct {
		nameEnd, exprEnd int
		val              string
	}
	var splices []pending
	for name, val := range set {
		attr, ok := blk.Body.Attributes[name]
		if !ok {
			continue
		}
		splices = append(splices, pending{
			nameEnd: int(attr.SrcRange.Start.Byte) + len(attr.Name),
			exprEnd: int(attr.Expr.Range().End.Byte),
			val:     val,
		})
	}
	sort.Slice(splices, func(i, j int) bool { return splices[i].nameEnd > splices[j].nameEnd })
	for _, s := range splices {
		data = spliceRange(data, s.nameEnd, s.exprEnd, s.val)
	}
	// Insert attributes that do not exist yet, in canonical order. Each
	// insertion lands before the first canonically-later existing attribute
	// (or the closing brace), so block shape stays stable.
	// Re-locate the block first: earlier splices shifted byte offsets.
	f, diags = hclsyntax.ParseConfig(data, ".terraform.lock.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	body, ok = f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("unexpected body type %T", f.Body)
	}
	blk = findSyntaxProviderBlock(body, addr)
	for _, name := range canonicalAttrs {
		val, isSet := set[name]
		if !isSet {
			continue
		}
		if _, exists := blk.Body.Attributes[name]; exists {
			continue
		}
		at := insertionPoint(data, blk, name)
		// Insert at the start of the line holding the insertion point,
		// reusing that line's indent — the indent bytes are already there.
		lineStart := at
		for lineStart > 0 && data[lineStart-1] != '\n' {
			lineStart--
		}
		indent := leadingIndent(data, at)
		insert := indent + name + " = " + val + "\n"
		data = append(append(append([]byte{}, data[:lineStart]...), insert...), data[lineStart:]...)
		blk = locateSyntaxBlockIn(data, addr)
		if blk == nil {
			return nil, fmt.Errorf("provider block %q vanished after edit", addr)
		}
	}
	return data, nil
}

// canonicalAttrs is the canonical attribute order for lock-file provider
// blocks: version, constraints, hashes.
var canonicalAttrs = []string{"version", "constraints", "hashes"}

// insertionPoint returns the byte offset where attr belongs: before the
// first canonically-later existing attribute, or just before the closing
// brace.
func insertionPoint(data []byte, blk *hclsyntax.Block, attr string) int {
	pos := sort.Search(len(canonicalAttrs), func(i int) bool { return canonicalAttrs[i] == attr })
	later := canonicalAttrs[pos+1:]
	for _, name := range later {
		if a, ok := blk.Body.Attributes[name]; ok {
			return int(a.SrcRange.Start.Byte)
		}
	}
	return int(blk.CloseBraceRange.Start.Byte)
}

func locateSyntaxBlockIn(data []byte, addr string) *hclsyntax.Block {
	f, diags := hclsyntax.ParseConfig(data, ".terraform.block.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil
	}
	return findSyntaxProviderBlock(body, addr)
}

// formatBlockOnly runs the target block through the hcl formatter and splices
// it back, leaving the rest of the file untouched.
func formatBlockOnly(data []byte, addr string) ([]byte, error) {
	blk, err := locateSyntaxBlock(data, addr)
	if err != nil {
		return nil, err
	}
	if blk == nil {
		return data, nil
	}
	start := int(blk.Range().Start.Byte)
	end := int(blk.Range().End.Byte)
	sub := data[start:end]
	f, diags := hclwrite.ParseConfig(sub, "provider", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, fmt.Errorf("formatting provider %s: %w", addr, diags)
	}
	formatted := bytes.TrimSuffix(f.Bytes(), []byte("\n"))
	out := make([]byte, 0, len(data)+len(formatted))
	out = append(out, data[:start]...)
	out = append(out, formatted...)
	out = append(out, data[end:]...)
	return out, nil
}

func locateSyntaxBlock(data []byte, addr string) (*hclsyntax.Block, error) {
	f, diags := hclsyntax.ParseConfig(data, ".terraform.lock.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("unexpected body type %T", f.Body)
	}
	return findSyntaxProviderBlock(body, addr), nil
}

func findSyntaxProviderBlock(body *hclsyntax.Body, addr string) *hclsyntax.Block {
	for _, b := range body.Blocks {
		if b.Type == "provider" && len(b.Labels) == 1 && b.Labels[0] == addr {
			return b
		}
	}
	return nil
}

func findProviderBlock(body *hclwrite.Body, addr string) *hclwrite.Block {
	for _, b := range body.Blocks() {
		if b.Type() == "provider" && len(b.Labels()) == 1 && b.Labels()[0] == addr {
			return b
		}
	}
	return nil
}

// spliceRange replaces data[nameEnd:exprEnd] — everything after the attribute
// name through the end of its expression — with " = val". Bytes before the
// name (indentation) and after the expression (trailing comments) survive.
func spliceRange(data []byte, nameEnd, exprEnd int, val string) []byte {
	out := make([]byte, 0, len(data)+len(val))
	out = append(out, data[:nameEnd]...)
	out = append(out, " = "...)
	out = append(out, val...)
	out = append(out, data[exprEnd:]...)
	return out
}

// leadingIndent returns the whitespace prefix of the line containing offset.
// If that prefix contains non-whitespace (close brace on the block header
// line, for example), it falls back to two spaces.
func leadingIndent(data []byte, offset int) string {
	lineStart := offset
	for lineStart > 0 && data[lineStart-1] != '\n' {
		lineStart--
	}
	prefix := data[lineStart:offset]
	for _, b := range prefix {
		if b != ' ' && b != '\t' {
			return "  "
		}
	}
	if len(prefix) == 0 {
		return "  "
	}
	return string(prefix)
}

func renderBlockPlain(addr string, attrs ProviderAttrs) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "provider %q {\n", addr)
	if attrs.Version != "" {
		sb.WriteString("  version = " + plainString(attrs.Version) + "\n")
	}
	if attrs.Constraints != "" {
		sb.WriteString("  constraints = " + plainString(attrs.Constraints) + "\n")
	}
	if len(attrs.Hashes) > 0 {
		sb.WriteString("  hashes = " + plainHashes(attrs.Hashes) + "\n")
	}
	sb.WriteString("}\n")
	return sb.String()
}

func plainString(s string) string {
	return strconv.Quote(s)
}

func plainHashes(hashes []string) string {
	quoted := make([]string, 0, len(hashes))
	for _, h := range hashes {
		quoted = append(quoted, plainString(h))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
