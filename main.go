package main

import (
	"container/heap"
	"flag"
	"fmt"
	"log"
	"math/bits"
	"time"
)

var border Bitmap

const n = 14

func main() {
	progressflag := flag.Bool("progress", false, "show progress")
	flag.Parse()

	for i := range border {
		if i < n {
			border[i] = 0xffff >> n << n
		} else {
			border[i] = 0xffff
		}
	}

	var progress <-chan time.Time
	if *progressflag {
		progress = time.Tick(1 * time.Second)
	}

	var g Generator
	g.border = border
	g.walls = border
	g.sink = Point{X: 1, Y: n - 1}
	g.startPos = Point{X: 1, Y: 1}
	g.progress = progress
	fmt.Println(g.walls.String())
	node := g.Search()
	fmt.Println(node.len)
	for n := node; n != nil; n = n.parent {
		fmt.Println(n.state.blocks.String())
		fmt.Println("-")
	}
	//pretty.Println(node.state)
}

type Generator struct {
	border   Bitmap // XXX delete?
	walls    Bitmap
	sink     Point // where the block have to go (come from)
	startPos Point

	progress <-chan time.Time
}

// Normal sokoban:
// a block can be pushed if square 2 is reachable and square 0 is not blocked
//
//     _  [] P  ->  [] P  _
//     0  1  2      0  1  2
//
// backwards sokoban:
// a block can be pulled if square 1 is reachable and square 2 is not blocked
//
//     [] P  _   ->  _  [] P
//     0  1  2       0  1  2

type Point struct{ Y, X int8 }

var dirs = [4]Point{
	{-1, 0}, {+1, 0}, {0, -1}, {0, +1},
}

func (g *Generator) Search() *node {
	var visited = make(map[state]*node)
	var queue nodeQueue // []*node
	var start = new(node)
	var max = start
	start.state.blocks.Set(g.sink.X, g.sink.Y, true)
	start.state.pos = g.startPos
	fmt.Println(start.state.blocks.String())
	queue = append(queue, start)
	for len(queue) > 0 {
		no := heap.Pop(&queue).(*node)
		if v := visited[no.state]; v != nil {
			continue
		}
		visited[no.state] = no

		if no.len > max.len {
			max = no
		}

		select {
		case <-g.progress:
			log.Printf("current: %d, max %d", no.len, max.len)
			fmt.Println(no.state.blocks.String())
		default:
		}

		// find reachable squares
		r := reachable(no.state.pos.X, no.state.pos.Y, &g.walls, &no.state.blocks)

		// find valid moves
		for i, bl := range no.state.blocks {
			/// TODO: maybe mask bl with r?

			// iterate over each block
			for bl != 0 {
				y := i
				x := bits.Len16(bl) - 1
				bl = bl & ((1 << x) - 1) // clear current block

				if !no.state.blocks.At(int8(x), int8(y)) {
					panic("block does not exist")
				}

				for _, d := range dirs {
					dx, dy := int(d.X), int(d.Y)

					if x+dx+dx < 0 || x+dx+dx > n {
						continue
					}
					if y+dy+dy < 0 || y+dy+dy > n {
						continue
					}

					// in order to pull,
					// two squares in the pull direction
					// must be reachable & clear
					if !r.At(int8(x+dx), int8(y+dy)) {
						continue
					}
					if !r.At(int8(x+dx+dx), int8(y+dy+dy)) {
						continue
					}

					new := &node{
						state:  no.state,
						parent: no,
						len:    no.len + 1,
					}
					// set the new block position
					new.state.blocks.Set(int8(x), int8(y), false)
					new.state.blocks.Set(int8(x+dx), int8(y+dy), true)

					// there is always a block at the sink
					new.state.blocks.Set(g.sink.X, g.sink.Y, true)

					// update pos
					// TODO: normalize to upper left reachable square
					new.state.pos.X = int8(x + dx + dx)
					new.state.pos.Y = int8(y + dy + dy)

					// add to the heap
					if v := visited[new.state]; v != nil {
						continue
					}
					heap.Push(&queue, new)
				}
			}
		}
	}
	log.Println("visited ", len(visited), "states")
	return max
}

// Return a bitmap of all squares reachable from x,y
// without visiting mask1 or mask2
func reachable(x, y int8, mask1, mask2 *Bitmap) Bitmap {
	var a Bitmap
	a.Set(x, y, true)
	for {
		prev := uint16(0)
		changed := uint16(0)
		for i := 0; i < len(a); i++ {
			tmp := a[i]
			tmp2 := tmp | tmp<<1 | tmp>>1 | prev
			if i+1 < len(a) {
				tmp2 |= a[i+1]
			}
			tmp2 &^= mask1[i]
			tmp2 &^= mask2[i]
			changed |= tmp2 &^ tmp
			a[i] |= tmp2
			prev = tmp
		}

		if changed == 0 {
			break
		}
	}
	return a
}

type node struct {
	state  state
	parent *node
	len    int
}

type state struct {
	blocks Bitmap
	pos    Point // position of player after last pull
}

type nodeQueue []*node

func (h nodeQueue) Len() int            { return len(h) }
func (h nodeQueue) Less(i, j int) bool  { return h[i].len < h[j].len }
func (h nodeQueue) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *nodeQueue) Push(x interface{}) { *h = append(*h, x.(*node)) }
func (h *nodeQueue) Pop() interface{} {
	x := (*h)[len(*h)-1]
	*h = (*h)[:len(*h)-1]
	return x
}
