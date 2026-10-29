package release

// DiffLine is one line of a unified diff hunk: "context", "add" or "remove".
type DiffLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type DiffHunk struct {
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Lines    []DiffLine `json:"lines"`
}

type LineDiff struct {
	Added    int        `json:"added"`
	Removed  int        `json:"removed"`
	Hunks    []DiffHunk `json:"hunks"`
	Replaced bool       `json:"replaced"` // too different to align; shown as a full replacement
}

type diffOp struct {
	kind byte // '=', '-', '+'
	text string
}

// maxDiffEdits bounds Myers' trace, which grows with the square of the edit count.
const maxDiffEdits = 2000

// DiffLines aligns two configurations line by line. Configuration changes are
// usually small and local, so the common prefix and suffix are trimmed first.
func DiffLines(a, b []string, context int) LineDiff {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	middleA, middleB := a[prefix:len(a)-suffix], b[prefix:len(b)-suffix]
	middle, ok := myers(middleA, middleB, maxDiffEdits)
	if !ok {
		middle = make([]diffOp, 0, len(middleA)+len(middleB))
		for _, line := range middleA {
			middle = append(middle, diffOp{'-', line})
		}
		for _, line := range middleB {
			middle = append(middle, diffOp{'+', line})
		}
	}
	ops := make([]diffOp, 0, prefix+len(middle)+suffix)
	for _, line := range a[:prefix] {
		ops = append(ops, diffOp{'=', line})
	}
	ops = append(ops, middle...)
	for _, line := range a[len(a)-suffix:] {
		ops = append(ops, diffOp{'=', line})
	}
	result := LineDiff{Replaced: !ok, Hunks: []DiffHunk{}}
	for _, op := range ops {
		switch op.kind {
		case '+':
			result.Added++
		case '-':
			result.Removed++
		}
	}
	result.Hunks = buildHunks(ops, context)
	return result
}

func myers(a, b []string, maxD int) ([]diffOp, bool) {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil, true
	}
	var trace [][]int // trace[d][k+d] is the furthest x on diagonal k after d edits
	var prev []int
	for d := 0; d <= maxD; d++ {
		cur := make([]int, 2*d+1)
		for k := -d; k <= d; k += 2 {
			var x int
			switch {
			case d == 0:
			case k == -d || (k != d && prev[k-1+d-1] < prev[k+1+d-1]):
				x = prev[k+1+d-1]
			default:
				x = prev[k-1+d-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			cur[k+d] = x
			if x >= n && y >= m {
				return backtrack(a, b, append(trace, cur)), true
			}
		}
		trace = append(trace, cur)
		prev = cur
	}
	return nil, false
}

func backtrack(a, b []string, trace [][]int) []diffOp {
	x, y := len(a), len(b)
	var ops []diffOp
	for d := len(trace) - 1; d > 0; d-- {
		prev := trace[d-1]
		k := x - y
		prevK := k - 1
		if k == -d || (k != d && prev[k-1+d-1] < prev[k+1+d-1]) {
			prevK = k + 1
		}
		prevX := prev[prevK+d-1]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			ops = append(ops, diffOp{'=', a[x-1]})
			x--
			y--
		}
		if x == prevX {
			ops = append(ops, diffOp{'+', b[y-1]})
			y--
		} else {
			ops = append(ops, diffOp{'-', a[x-1]})
			x--
		}
	}
	for x > 0 && y > 0 {
		ops = append(ops, diffOp{'=', a[x-1]})
		x--
		y--
	}
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
	return ops
}

func buildHunks(ops []diffOp, context int) []DiffHunk {
	oldNo, newNo := make([]int, len(ops)), make([]int, len(ops))
	o, n := 1, 1
	var changes []int
	for i, op := range ops {
		oldNo[i], newNo[i] = o, n
		if op.kind != '+' {
			o++
		}
		if op.kind != '-' {
			n++
		}
		if op.kind != '=' {
			changes = append(changes, i)
		}
	}
	hunks := []DiffHunk{}
	for i := 0; i < len(changes); {
		first, last := changes[i], changes[i]
		for i+1 < len(changes) && changes[i+1]-last <= 2*context+1 {
			i++
			last = changes[i]
		}
		i++
		start, end := max(0, first-context), min(len(ops)-1, last+context)
		hunk := DiffHunk{OldStart: oldNo[start], NewStart: newNo[start]}
		for _, op := range ops[start : end+1] {
			switch op.kind {
			case '=':
				hunk.Lines = append(hunk.Lines, DiffLine{"context", op.text})
				hunk.OldLines++
				hunk.NewLines++
			case '-':
				hunk.Lines = append(hunk.Lines, DiffLine{"remove", op.text})
				hunk.OldLines++
			case '+':
				hunk.Lines = append(hunk.Lines, DiffLine{"add", op.text})
				hunk.NewLines++
			}
		}
		hunks = append(hunks, hunk)
	}
	return hunks
}
