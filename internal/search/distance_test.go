package search

import "testing"

func TestDamerauLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		// Identical strings.
		{"", "", 0},
		{"abc", "abc", 0},
		{"grep", "grep", 0},

		// Empty vs non-empty.
		{"", "abc", 3},
		{"abc", "", 3},

		// Single edit operations.
		{"cat", "cats", 1}, // insertion
		{"cats", "cat", 1}, // deletion
		{"cat", "car", 1},  // substitution
		{"ab", "ba", 1},    // transposition (Damerau, not standard Levenshtein)

		// Transposition — distinguishes from standard Levenshtein.
		{"grep", "grpe", 1}, // adjacent transposition
		{"abcd", "abdc", 1}, // adjacent transposition at end

		// Multiple edits.
		{"kitten", "sitting", 3},
		{"sunday", "saturday", 3},
		{"abc", "xyz", 3},

		// Realistic manpage typos.
		{"systemctl", "sytemctl", 1}, // missing 's'
		{"iptables", "iptabels", 1},  // transposition
		{"crontab", "contrab", 2},    // transposition + substitution
	}
	for _, tc := range tests {
		got := damerauLevenshtein(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("damerauLevenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestDamerauLevenshtein_Symmetric(t *testing.T) {
	pairs := [][2]string{
		{"abc", "abd"},
		{"grep", "grpe"},
		{"kitten", "sitting"},
		{"", "hello"},
	}
	for _, p := range pairs {
		ab := damerauLevenshtein(p[0], p[1])
		ba := damerauLevenshtein(p[1], p[0])
		if ab != ba {
			t.Errorf("damerauLevenshtein(%q, %q) = %d but reverse = %d", p[0], p[1], ab, ba)
		}
	}
}

func TestFuzzyThreshold(t *testing.T) {
	tests := []struct {
		queryLen int
		want     int
	}{
		{1, 0},
		{2, 0},
		{3, 1},
		{4, 1},
		{5, 2},
		{7, 2},
		{8, 2},
		{9, 2},
		{15, 2},
	}
	for _, tc := range tests {
		got := fuzzyThreshold(tc.queryLen)
		if got != tc.want {
			t.Errorf("fuzzyThreshold(%d) = %d, want %d", tc.queryLen, got, tc.want)
		}
	}
}

func TestDamerauLevenshteinBounded(t *testing.T) {
	// When maxDist is large enough, result matches the unbounded version.
	pairs := [][2]string{
		{"abc", "abc"},
		{"grep", "grpe"},
		{"kitten", "sitting"},
		{"cat", "cats"},
		{"ab", "ba"},
	}
	for _, p := range pairs {
		unbounded := damerauLevenshtein(p[0], p[1])
		bounded := damerauLevenshteinBounded(p[0], p[1], unbounded+5)
		if bounded != unbounded {
			t.Errorf("bounded(%q, %q, %d) = %d, want %d", p[0], p[1], unbounded+5, bounded, unbounded)
		}
	}

	// With tight maxDist, returns maxDist+1 for strings exceeding the threshold.
	if got := damerauLevenshteinBounded("abc", "xyz", 1); got != 2 {
		t.Errorf("bounded(abc, xyz, 1) = %d, want 2", got)
	}

	// Length pre-filter: strings with length difference > maxDist bail immediately.
	if got := damerauLevenshteinBounded("a", "abcde", 2); got != 3 {
		t.Errorf("bounded(a, abcde, 2) = %d, want 3", got)
	}

	// Edge cases.
	if got := damerauLevenshteinBounded("", "abc", 1); got != 3 {
		t.Errorf("bounded('', abc, 1) = %d, want 3", got)
	}
	if got := damerauLevenshteinBounded("abc", "", 1); got != 3 {
		t.Errorf("bounded(abc, '', 1) = %d, want 3", got)
	}

	// Within threshold returns exact distance.
	if got := damerauLevenshteinBounded("grep", "grpe", 1); got != 1 {
		t.Errorf("bounded(grep, grpe, 1) = %d, want 1", got)
	}
	if got := damerauLevenshteinBounded("grep", "grpe", 2); got != 1 {
		t.Errorf("bounded(grep, grpe, 2) = %d, want 1", got)
	}
}

func BenchmarkDamerauLevenshtein(b *testing.B) {
	for i := 0; i < b.N; i++ {
		damerauLevenshtein("systemctl", "sytemctl")
	}
}

func BenchmarkDamerauLevenshteinBounded(b *testing.B) {
	for i := 0; i < b.N; i++ {
		damerauLevenshteinBounded("systemctl", "sytemctl", 1)
	}
}

func BenchmarkDamerauLevenshteinBoundedReject(b *testing.B) {
	// Dissimilar strings — bounded should return early.
	for i := 0; i < b.N; i++ {
		damerauLevenshteinBounded("systemctl", "completely_different", 2)
	}
}
