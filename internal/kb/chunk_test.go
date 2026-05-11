package kb

import (
	"strings"
	"testing"
)

func TestChunkShortPassesThrough(t *testing.T) {
	got := Chunk("hello world", 800, 80)
	if len(got) != 1 || got[0] != "hello world" {
		t.Errorf("got %+v", got)
	}
}

func TestChunkSplitsOnParagraphs(t *testing.T) {
	body := strings.Repeat("para a.\n\n", 50) + strings.Repeat("para b.\n\n", 50)
	got := Chunk(body, 200, 30)
	if len(got) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(got))
	}
	for _, c := range got {
		if len(c) > 250 { // soft check; overlap may add some headroom
			t.Errorf("chunk too large: %d chars", len(c))
		}
	}
}

func TestChunkSplitsOverlongParagraph(t *testing.T) {
	body := strings.Repeat("x", 5000)
	got := Chunk(body, 800, 80)
	if len(got) < 5 {
		t.Errorf("expected several windows, got %d", len(got))
	}
}

func TestChunkEmpty(t *testing.T) {
	if got := Chunk("   ", 100, 10); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}
