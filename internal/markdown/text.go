package markdown

import (
	gast "github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"

	mast "github.com/usememos/memos/internal/markdown/ast"
)

// TextChunk is a piece of plain text extracted from markdown content, with
// the byte range of the source content it was extracted from.
type TextChunk struct {
	// Text is the extracted plain text.
	Text string
	// SourceStart and SourceEnd are the byte offsets of the source content
	// range the text was extracted from. HasSource is false for chunks with
	// no exact source range (for example autolink URLs rebuilt from a
	// protocol prefix).
	SourceStart int
	SourceEnd   int
	HasSource   bool
	// Block is true when the chunk starts a new block-level element (or
	// follows a line break), marking where a separator belongs when the
	// chunks are joined into one text.
	Block bool
}

// ExtractText extracts the plain text of markdown content as ordered chunks
// with source byte ranges. It is the raw material for the search corpus
// projection: unlike GenerateSnippet it keeps the full text, includes code
// block content, and records where each chunk came from.
func (s *service) ExtractText(content []byte) ([]TextChunk, error) {
	root, err := s.parse(content)
	if err != nil {
		return nil, err
	}

	chunks := []TextChunk{}
	// atBlockStart marks that the next emitted chunk begins a new block.
	atBlockStart := false

	err = gast.Walk(root, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}

		switch n.Kind() {
		case gast.KindParagraph, gast.KindHeading, gast.KindListItem,
			east.KindTableCell, east.KindTableRow, east.KindTableHeader:
			atBlockStart = true
			return gast.WalkContinue, nil
		case gast.KindFencedCodeBlock, gast.KindCodeBlock:
			// Code blocks have no inline children; emit their lines as one
			// chunk spanning the block's source range.
			if lines := n.Lines(); lines != nil && lines.Len() > 0 {
				first := lines.At(0)
				last := lines.At(lines.Len() - 1)
				chunks = append(chunks, TextChunk{
					Text:        string(content[first.Start:last.Stop]),
					SourceStart: first.Start,
					SourceEnd:   last.Stop,
					HasSource:   true,
					Block:       true,
				})
			}
			return gast.WalkSkipChildren, nil
		default:
			// Fall through to inline text extraction below.
		}

		var chunk TextChunk
		lineBreak := false
		skipChildren := false
		switch node := n.(type) {
		case *gast.Text:
			segment := node.Segment
			chunk = TextChunk{
				Text:        string(content[segment.Start:segment.Stop]),
				SourceStart: segment.Start,
				SourceEnd:   segment.Stop,
				HasSource:   true,
			}
			lineBreak = node.SoftLineBreak() || node.HardLineBreak()
		case *gast.AutoLink:
			// AutoLink keeps its source segment unexported; the URL is
			// emitted without a source range.
			chunk = TextChunk{Text: string(node.URL(content))}
			skipChildren = true
		case *mast.TagNode:
			chunk = TextChunk{
				Text:        "#" + string(node.Tag),
				SourceStart: node.Segment.Start,
				SourceEnd:   node.Segment.Stop,
				HasSource:   true,
			}
		default:
			return gast.WalkContinue, nil
		}

		if chunk.Text != "" {
			chunk.Block = atBlockStart
			atBlockStart = false
			chunks = append(chunks, chunk)
		}
		if lineBreak {
			// A line break separates the following chunk from this one.
			atBlockStart = true
		}
		if skipChildren {
			return gast.WalkSkipChildren, nil
		}
		return gast.WalkContinue, nil
	})
	if err != nil {
		return nil, err
	}

	return chunks, nil
}
