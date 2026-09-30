package cli

import "sort"

// didYouMean is clap's suggestions::did_you_mean: every candidate with
// a Jaro similarity above 0.7, least similar first (callers take the
// last for a single suggestion).
func didYouMean(value string, candidates []string) []string {
	type scored struct {
		score float64
		name  string
	}
	var found []scored
	for _, c := range candidates {
		if s := jaro(value, c); s > 0.7 {
			found = append(found, scored{s, c})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].score < found[j].score })
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.name
	}
	return out
}

// jaro is strsim::jaro over chars.
func jaro(a, b string) float64 {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 && len(br) == 0 {
		return 1
	}
	if len(ar) == 0 || len(br) == 0 {
		return 0
	}
	searchRange := max(len(ar), len(br))/2 - 1
	searchRange = max(searchRange, 0)
	aFlags := make([]bool, len(ar))
	bFlags := make([]bool, len(br))
	matches := 0
	for i, ac := range ar {
		lo := max(i-searchRange, 0)
		hi := min(len(br), i+searchRange+1)
		for j := lo; j < hi; j++ {
			if ac == br[j] && !bFlags[j] {
				aFlags[i], bFlags[j] = true, true
				matches++
				break
			}
		}
	}
	if matches == 0 {
		return 0
	}
	transpositions := 0
	j := 0
	for i, ac := range ar {
		if !aFlags[i] {
			continue
		}
		for !bFlags[j] {
			j++
		}
		if ac != br[j] {
			transpositions++
		}
		j++
	}
	transpositions /= 2
	m := float64(matches)
	return (m/float64(len(ar)) + m/float64(len(br)) + (m-float64(transpositions))/m) / 3
}
