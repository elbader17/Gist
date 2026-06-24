package diff

import (
	"strings"
	"testing"
)

func TestParseNum(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"+10", 10},
		{"-5", 5},
		{"0", 0},
		{"+12,3", 12},
		{"", 0},
		{"abc", 0},
	}
	for _, c := range cases {
		if got := parseNum(c.in); got != c.want {
			t.Errorf("parseNum(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"foo.go", "modified"},
		{"R foo", "renamed"},
		{"M bar", "renamed"},
		{"noext", "modified"},
	}
	for _, c := range cases {
		if got := classify(c.path); got != c.want {
			t.Errorf("classify(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestParseStat(t *testing.T) {
	out := "10\t5\tfile.go\n3\t1\tfile2.go\n"
	files := parseStat(out)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Path != "file.go" {
		t.Errorf("path = %q", files[0].Path)
	}
	if files[0].AddedLines != 10 {
		t.Errorf("added = %d", files[0].AddedLines)
	}
	if files[0].RemovedLines != 5 {
		t.Errorf("removed = %d", files[0].RemovedLines)
	}
	if files[1].AddedLines != 3 {
		t.Errorf("file2 added = %d", files[1].AddedLines)
	}
}

func TestParseNumStatBinaries(t *testing.T) {
	out := "-\t-\tbinary.png\n10\t2\tfile.go\n"
	files := parseNumStat(out)
	if len(files) != 1 {
		t.Fatalf("binary entries (- -) should be skipped, got %d", len(files))
	}
	if files[0].Path != "file.go" {
		t.Errorf("expected file.go, got %q", files[0].Path)
	}
}

func TestParseStatEmpty(t *testing.T) {
	if files := parseStat(""); len(files) != 0 {
		t.Errorf("empty should yield 0 files, got %d", len(files))
	}
}

func TestExtractFunctions(t *testing.T) {
	patch := `diff --git a/x.go b/x.go
@@ -1,5 +1,5 @@
+func NewThing() {}
+func Other() {}
+func NewThing() {}
`
	got := extractFunctions(patch)
	want := map[string]bool{"NewThing": false, "Other": false}
	for _, fn := range got {
		if _, ok := want[fn]; ok {
			want[fn] = true
		}
	}
	if !want["NewThing"] || !want["Other"] {
		t.Errorf("expected NewThing and Other, got %v", got)
	}
	for _, fn := range got {
		if strings.Count(fn, "NewThing") > 1 {
			t.Error("duplicates not deduped")
		}
	}
}

func TestExtractFunctionsTypes(t *testing.T) {
	patch := `+type Foo struct { x int }
+type Bar interface { Run() }
`
	got := extractFunctions(patch)
	found := false
	for _, fn := range got {
		if strings.HasPrefix(fn, "Bar") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Bar type extracted, got %v", got)
	}
}

func TestIsLogOnly(t *testing.T) {
	logPatch := `+	fmt.Println("a")
+	fmt.Printf("b")
`
	if !isLogOnly(logPatch) {
		t.Error("patch with only Println/Printf should be log-only")
	}

	mixedPatch := `+	fmt.Println("a")
+	x := 1
`
	if isLogOnly(mixedPatch) {
		t.Error("patch with mixed lines should not be log-only")
	}
}

func TestIsLogOnlyEmpty(t *testing.T) {
	if isLogOnly("") {
		t.Error("empty patch should not be log-only")
	}
}

func TestIsCommentOnly(t *testing.T) {
	cases := []struct {
		name  string
		patch string
		want  bool
	}{
		{
			"only comments",
			"+// hello\n+// world\n",
			true,
		},
		{
			"hash comments",
			"+# bash comment\n+# another\n",
			true,
		},
		{
			"mixed",
			"+// hello\n+x := 1\n",
			false,
		},
		{
			"empty",
			"",
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isCommentOnly(c.patch); got != c.want {
				t.Errorf("isCommentOnly(%q) = %v, want %v", c.patch, got, c.want)
			}
		})
	}
}

func TestDescribe(t *testing.T) {
	cases := []struct {
		name string
		fc   *FileChange
		want string
	}{
		{"log", &FileChange{Path: "x.go", LogOnly: true}, "x.go: only log/print additions"},
		{"comment", &FileChange{Path: "x.go", CommentOnly: true}, "x.go: only comment additions"},
		{"functions", &FileChange{Path: "x.go", Functions: []string{"Foo", "Bar"}}, "x.go: modified Foo, Bar"},
		{"lines", &FileChange{Path: "x.go", AddedLines: 5, RemovedLines: 3}, "x.go: +5 -3 lines"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := describe(c.fc); got != c.want {
				t.Errorf("describe = %q, want %q", got, c.want)
			}
		})
	}
}