package session

import "math"

// sep is the width (in cells) of the vertical separator drawn between panes
// in the same row.
const sep = 1

// paneBox describes where a pane's content lives on screen. The pane's title
// bar occupies the single row directly above the content (at y-1).
type paneBox struct {
	x, y int // absolute top-left of the content area
	w, h int // content dimensions
}

// grid picks a tile arrangement for n panes: roughly square, columns first.
func grid(n int) (cols, rows int) {
	if n < 1 {
		return 1, 1
	}
	cols = int(math.Ceil(math.Sqrt(float64(n))))
	rows = int(math.Ceil(float64(n) / float64(cols)))
	return cols, rows
}

// distribute splits total into parts pieces, handing the remainder to the
// first pieces so the sum is exactly total. Each piece is at least 1.
func distribute(total, parts int) []int {
	out := make([]int, parts)
	if parts <= 0 {
		return out
	}
	base := total / parts
	extra := total % parts
	for i := range out {
		out[i] = base
		if i < extra {
			out[i]++
		}
		if out[i] < 1 {
			out[i] = 1
		}
	}
	return out
}

// layout tiles n panes into the given content area (width x height, where
// height already excludes the status line). It returns one box per pane in
// row-major order, each carrying the absolute content origin so callers can
// place a cursor precisely.
func layout(n, width, height int) []paneBox {
	if n < 1 {
		return nil
	}
	cols, rows := grid(n)

	// Each row spends one line on its pane titles.
	rowHeights := distribute(height-rows, rows)

	boxes := make([]paneBox, 0, n)
	y := 0
	for r := 0; r < rows; r++ {
		panesInRow := cols
		if remaining := n - r*cols; remaining < cols {
			panesInRow = remaining
		}
		// Width spent on separators is shared by the panes in this row.
		colWidths := distribute(width-(panesInRow-1)*sep, panesInRow)

		contentY := y + 1 // title sits on row y
		x := 0
		for c := 0; c < panesInRow; c++ {
			boxes = append(boxes, paneBox{
				x: x,
				y: contentY,
				w: colWidths[c],
				h: rowHeights[r],
			})
			x += colWidths[c] + sep
		}
		y = contentY + rowHeights[r]
	}
	return boxes
}
