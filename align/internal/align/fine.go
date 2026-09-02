package align

import "math"

// Pass 2: fine alignment.
//
// The coarse pass says a window of narration starts somewhere near
// canonical token N. This pass says exactly which canonical token each
// spoken token is, by running a word-level alignment of the window against
// the canonical region around N.
//
// The alignment is a fit alignment — the whole window is aligned, the
// reference region's ends are free — and it is banded: only cells within
// bandHalfWidth tokens of the expected diagonal are computed. That is what
// keeps it O(n·k) instead of O(n·m), and it is not merely an optimization.
// A cell far off the diagonal represents the narrator having skipped or
// repeated hundreds of words inside one minute, which does not happen; a
// full matrix would spend most of its time evaluating that.

const (
	dirNone = iota
	dirDiag // the spoken token is this canonical token
	dirUp   // the spoken token has no canonical counterpart
	dirLeft // this canonical token was not spoken
)

const (
	scoreMatch    = 2.0
	scoreMismatch = -1.0
	scoreGap      = -1.0
)

// fitAlign aligns every token of q against the reference r, expecting q[0]
// to sit near r[diag]. It returns one entry per q token: the index in r it
// aligned to, or -1 for a spoken token with no counterpart.
func fitAlign(q, r []matchTok, diag, band int) []int {
	out := filled(len(q), -1)
	if len(q) == 0 || len(r) == 0 {
		return out
	}

	width := 2*band + 1
	// lo(i) is the first reference index reachable on row i.
	lo := func(i int) int { return clampInt(i+diag-band, 0, len(r)) }
	hi := func(i int) int { return clampInt(i+diag+band, 0, len(r)) }

	const negInf = -math.MaxFloat64
	prev := make([]float64, width+1)
	cur := make([]float64, width+1)
	dirs := make([]uint8, (len(q)+1)*(width+1))

	// Row 0: a free reference prefix, so the window may begin anywhere in
	// the band without paying for the canonical text that came before it.
	prevLo, prevHi := lo(0), hi(0)
	for j := prevLo; j <= prevHi; j++ {
		prev[j-prevLo] = 0
	}

	for i := 1; i <= len(q); i++ {
		curLo, curHi := lo(i), hi(i)
		for j := curLo; j <= curHi; j++ {
			bestScore, bestDir := negInf, uint8(dirNone)
			if j-1 >= prevLo && j-1 <= prevHi {
				s := scoreMismatch
				if q[i-1].Word == r[j-1].Word {
					s = scoreMatch
				}
				if v := prev[j-1-prevLo] + s; v > bestScore {
					bestScore, bestDir = v, dirDiag
				}
			}
			if j >= prevLo && j <= prevHi {
				if v := prev[j-prevLo] + scoreGap; v > bestScore {
					bestScore, bestDir = v, dirUp
				}
			}
			if j-1 >= curLo {
				if v := cur[j-1-curLo] + scoreGap; v > bestScore {
					bestScore, bestDir = v, dirLeft
				}
			}
			cur[j-curLo] = bestScore
			dirs[i*(width+1)+(j-curLo)] = bestDir
		}
		prev, cur = cur, prev
		prevLo, prevHi = curLo, curHi
	}

	// The last row's best cell is the end of the alignment: a free
	// reference suffix, matching the free prefix.
	bestJ, bestScore := -1, negInf
	for j := prevLo; j <= prevHi; j++ {
		if v := prev[j-prevLo]; v > bestScore {
			bestScore, bestJ = v, j
		}
	}
	if bestJ < 0 || bestScore == negInf {
		return out
	}

	i, j := len(q), bestJ
	for i > 0 {
		curLo := lo(i)
		if j < curLo || j > hi(i) {
			break
		}
		switch dirs[i*(width+1)+(j-curLo)] {
		case dirDiag:
			out[i-1] = j - 1
			i, j = i-1, j-1
		case dirUp:
			i--
		case dirLeft:
			j--
		default:
			return out
		}
	}
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
