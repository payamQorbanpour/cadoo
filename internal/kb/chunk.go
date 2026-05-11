package kb

import "strings"

// DefaultChunkSize is the target character size of a chunk.
const DefaultChunkSize = 800

// DefaultOverlap is how many trailing characters of a chunk are repeated at
// the start of the next, so semantic units that straddle a boundary are
// findable from either side.
const DefaultOverlap = 80

// Chunk splits body into overlapping windows of about size characters.
// Splits on paragraph (\n\n) boundaries when possible. If size <= 0 it falls
// back to DefaultChunkSize.
func Chunk(body string, size, overlap int) []string {
	if size <= 0 {
		size = DefaultChunkSize
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size / 4
	}
	body = strings.TrimSpace(body)
	if len(body) <= size {
		if body == "" {
			return nil
		}
		return []string{body}
	}

	var out []string
	paras := strings.Split(body, "\n\n")
	var cur strings.Builder
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Paragraph itself exceeds size — fall back to char-window splitting
		// for this paragraph alone.
		if len(p) > size {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			out = append(out, charWindows(p, size, overlap)...)
			continue
		}
		// If adding this paragraph would overshoot, flush.
		if cur.Len()+2+len(p) > size && cur.Len() > 0 {
			out = append(out, cur.String())
			// Seed the next chunk with the trailing overlap of the previous.
			tail := tailString(cur.String(), overlap)
			cur.Reset()
			cur.WriteString(tail)
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(p)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// charWindows splits a single overlong paragraph into overlapping windows.
func charWindows(s string, size, overlap int) []string {
	var out []string
	step := size - overlap
	if step < 1 {
		step = size
	}
	for i := 0; i < len(s); i += step {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[i:end])
		if end == len(s) {
			break
		}
	}
	return out
}

func tailString(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
