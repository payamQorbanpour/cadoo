package resolveconflicts

import (
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestHasConflictsDetectsMarker(t *testing.T) {
	cases := []struct {
		name  string
		files []vcs.FileChange
		want  bool
	}{
		{"clean diff", []vcs.FileChange{{Patch: "+ added\n- removed"}}, false},
		{"marker present", []vcs.FileChange{{Patch: "+<<<<<<< HEAD\n+foo\n+=======\n+bar\n+>>>>>>> branch"}}, true},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasConflicts(tc.files); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}
