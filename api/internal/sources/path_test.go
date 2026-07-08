package sources

import "testing"

func TestCleanRelPath(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "simple filename", in: "notes.md", want: "notes.md"},
		{name: "nested path", in: "docs/guide/intro.md", want: "docs/guide/intro.md"},
		{name: "backslashes normalized", in: `docs\guide\intro.md`, want: "docs/guide/intro.md"},
		{name: "leading slash stripped", in: "/docs/intro.md", want: "docs/intro.md"},
		{name: "redundant segments cleaned", in: "docs/./intro.md", want: "docs/intro.md"},
		{name: "empty", in: "", wantErr: true},
		{name: "traversal", in: "../secret", wantErr: true},
		{name: "nested traversal", in: "docs/../../secret", wantErr: true},
		{name: "dot", in: ".", wantErr: true},
		{name: "absolute after clean", in: "/../etc/passwd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cleanRelPath(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("cleanRelPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
