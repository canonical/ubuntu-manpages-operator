package search

// damerauLevenshtein computes the Damerau-Levenshtein distance between two
// strings using the optimal string alignment variant. It counts the minimum
// number of insertions, deletions, substitutions, and adjacent transpositions
// needed to transform a into b.
func damerauLevenshtein(a, b string) int {
	la, lb := len(a), len(b)
	mx := la
	if lb > mx {
		mx = lb
	}
	return damerauLevenshteinBounded(a, b, mx)
}

// damerauLevenshteinBounded is like damerauLevenshtein but returns early with
// maxDist+1 when the result is guaranteed to exceed maxDist. This is
// significantly faster when comparing dissimilar strings because it can bail
// out after the first few rows.
func damerauLevenshteinBounded(a, b string, maxDist int) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// The distance is at least |len(a) - len(b)|, so bail early if that
	// already exceeds the threshold.
	diff := la - lb
	if diff < 0 {
		diff = -diff
	}
	if diff > maxDist {
		return maxDist + 1
	}

	// Allocate two full rows plus a "previous-previous" row for transpositions.
	prev2 := make([]int, lb+1)
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}

			// Minimum of deletion, insertion, substitution.
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			best := del
			if ins < best {
				best = ins
			}
			if sub < best {
				best = sub
			}

			// Transposition of two adjacent characters.
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				trans := prev2[j-2] + cost
				if trans < best {
					best = trans
				}
			}

			curr[j] = best
			if best < rowMin {
				rowMin = best
			}
		}

		// If every value in this row exceeds maxDist, the final result
		// cannot be within the threshold — return early.
		if rowMin > maxDist {
			return maxDist + 1
		}

		// Rotate rows.
		prev2, prev, curr = prev, curr, prev2
	}

	return prev[lb]
}

// fuzzyThreshold returns the maximum Damerau-Levenshtein distance allowed for
// a query of the given length. Short queries use a tighter threshold to avoid
// excessive noise:
//
//	len ≤ 2  → 0 (fuzzy disabled)
//	len 3–4  → 1
//	len 5–8  → 2
//	len ≥ 9  → 2
func fuzzyThreshold(queryLen int) int {
	switch {
	case queryLen <= 2:
		return 0
	case queryLen <= 4:
		return 1
	default:
		return 2
	}
}
