package main

import (
	"container/heap"
	"log"
	"math/bits"
	"runtime"
	"time"
)

type Solver struct {
	// TODO: add a separate field for unenterable tiles
	// and let walls just be walls
	walls    Bitmap
	toggle   [2]Bitmap // toggle walls/floors
	blocks   Bitmap
	dead     Bitmap
	sink     Point // where the block have to go (come from)
	startPos Point
	button   Point // toggle button pos

	count    [256]int
	progress <-chan time.Time
}

func (g *Solver) Init(level *Level) {
	g.walls = Bitmap{}
	foundPlayer := false
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			p := Point{X: int8(x), Y: int8(y)}
			switch level.Tiles[y][x] {
			case Wall:
				g.walls.Set(p.X, p.Y, true)
			case Block:
				g.blocks.Set(p.X, p.Y, true)
			case Teleport:
				g.sink = p
			case Player:
				g.startPos = p
				foundPlayer = true
			case ToggleWall:
				g.toggle[0].Set(p.X, p.Y, true)
			case ToggleFloor:
				g.toggle[1].Set(p.X, p.Y, true)
			case ToggleButton:
				g.button = p
			}
		}
	}
	if !foundPlayer {
	loop:
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if level.Tiles[y][x] == Floor {
					g.startPos.X = int8(x)
					g.startPos.Y = int8(y)
					break loop
				}
			}
		}
	}
}

func (g *Solver) Search() *node {
	g.findDeadSquares()
	log.Printf("dead squares:\n%s", g.dead.String())

	var nogo [2]Bitmap
	nogo[0] = g.walls.Union(g.toggle[0])
	nogo[1] = g.walls.Union(g.toggle[1])

	var visited = make(map[state]struct{})
	var queue nodeQueue // []*node

	// init the queue with two start states:
	// one for each toggle state
	for t := uint8(0); t < 2; t++ {
		start := new(node)
		start.state.pos = g.startPos
		start.state.blocks = g.blocks
		start.state.toggle = t
		start.state.normalize(&nogo[t])
		heap.Push(&queue, start)
		log.Print("\n", g.formatLevel(start))
	}

	defer func() {
		log.Println("visited ", len(visited), "states")
	}()

	for len(queue) > 0 {
		no := heap.Pop(&queue).(*node)
		if _, ok := visited[no.state]; ok {
			continue
		}
		visited[no.state] = struct{}{}
		if no.len < len(g.count) {
			g.count[no.len]++
		}

		if no.state.nblocks() <= 0 {
			return no
		}

		select {
		case <-g.progress:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			log.Printf("alloc: current %d MB, max %d MB, sys %d MB", m.Alloc/1e6, m.TotalAlloc/1e6, m.Sys/1e6)
			log.Printf("search: current %d, visited: %d, queue %d\n%s", no.len, len(visited), len(queue),
				no.state.blocks.String())
		default:
		}

		// find reachable squares
		r := reachable(no.state.pos.X, no.state.pos.Y, &nogo[no.state.toggle], &no.state.blocks)

		// TODO:
		// - can flick blocks off toggle walls in MSCC
		// - let blocks press the button

		if r.At(g.button.X, g.button.Y) {
			new := newnode()
			*new = node{
				state:  no.state,
				parent: no,
				len:    no.len + 1,
			}

			// flip the toggle walls
			new.state.toggle ^= 1

			// update pos & normalize
			new.state.pos = g.button
			new.state.normalize(&nogo[new.state.toggle])

			heap.Push(&queue, new)
		}

		// find valid moves
		for i, bl := range no.state.blocks {
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

					// square beside the block must be
					// reachable and not blocked
					if x-dx < 0 || x-dx >= width {
						continue
					}
					if y-dy < 0 || y-dy >= height {
						continue
					}
					if !r.At(int8(x-dx), int8(y-dy)) {
						continue
					}

					// Square on the opposite side must be clear and not dead
					// (but not necessarily reachable)
					if x+dx < 0 || x+dx >= width {
						continue
					}
					if y+dy < 0 || y+dy >= height {
						continue
					}
					if nogo[no.state.toggle].At(int8(x+dx), int8(y+dy)) {
						continue
					}
					if no.state.blocks.At(int8(x+dx), int8(y+dy)) {
						continue
					}
					if g.dead.At(int8(x+dx), int8(y+dy)) {
						continue
					}

					new := newnode()
					*new = node{
						state:  no.state,
						parent: no,
						len:    no.len + 1,
					}
					// set the new block position
					new.state.blocks.Set(int8(x), int8(y), false)
					if !(int8(x+dx) == g.sink.X && int8(y+dy) == g.sink.Y) {
						new.state.blocks.Set(int8(x+dx), int8(y+dy), true)
					}

					// update pos
					new.state.pos.X = int8(x)
					new.state.pos.Y = int8(y)

					new.state.normalize(&nogo[new.state.toggle])

					// add to the heap
					if _, seen := visited[new.state]; !seen {
						heap.Push(&queue, new)
					}
				}
			}
		}
	}
	return nil
}

func (g *Solver) formatLevel(n *node) string {
	var s []byte
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			closed := n.state.toggle
			if g.walls.At(int8(x), int8(y)) {
				s = append(s, "##"...)
			} else if n.state.blocks.At(int8(x), int8(y)) {
				c := byte(']')
				if g.toggle[closed].At(int8(x), int8(y)) {
					c = '%'
				} else if g.toggle[closed^1].At(int8(x), int8(y)) {
					c = ':'
				}
				s = append(s, '[', c)
			} else if g.toggle[closed].At(int8(x), int8(y)) {
				s = append(s, "%%"...)
			} else if g.toggle[closed^1].At(int8(x), int8(y)) {
				s = append(s, "::"...)
			} else if x == int(n.state.pos.X) && y == int(n.state.pos.Y) {
				s = append(s, "$ "...)
			} else {
				if y%2 == 0 {
					s = append(s, ". "...)
				} else {
					s = append(s, " ,"...)
				}
			}
		}
		s = append(s, '\n')
	}
	return string(s)
}

// updates g.dead with squares that a block cannot occupy without getting stuck
func (g *Solver) findDeadSquares() {
	var nogo [2]Bitmap
	nogo[0] = g.walls.Union(g.toggle[0])
	nogo[1] = g.walls.Union(g.toggle[1])

	type node struct {
		block  Point
		pos    Point
		toggle uint8
	}

	var visited = make(map[node]struct{})
	var queue []node

	// init the queue with two start states:
	// one for each toggle state
	for t := uint8(0); t < 2; t++ {
		var start node
		start.block = g.sink
		start.pos = g.startPos
		start.toggle = t
		queue = append(queue, start)
	}

	// simple algorithm: pull a block until we run out of new states
	// for each state we visit, mark the safe bitmap
	// the queue is a FIFO because it doesn't really matter what order we visit in
	var safe Bitmap

	for len(queue) > 0 {
		var no node = queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if _, ok := visited[no]; ok {
			continue
		}
		visited[no] = struct{}{}

		safe.Set(no.block.X, no.block.Y, true)

		// find reachable squares
		var blocks Bitmap
		blocks.Set(no.block.X, no.block.Y, true)
		r := reachable(no.pos.X, no.pos.Y, &nogo[no.toggle], &blocks)

		if r.At(g.button.X, g.button.Y) {
			new := no
			new.toggle ^= 1
			new.pos = g.button
			queue = append(queue, new)
		}

		for _, d := range dirs {
			x, y := int(no.block.X), int(no.block.Y)
			dx, dy := int(d.X), int(d.Y)

			// two squares in the pull direction must be reachable
			x2, y2 := x+dx*2, y+dy*2
			if x2 < 0 || x2 >= width {
				continue
			}
			if y2 < 0 || y2 >= height {
				continue
			}
			if !r.At(int8(x2), int8(y2)) {
				continue
			}
			if !r.At(int8(x+dx), int8(y+dy)) {
				continue
			}

			new := no
			// set the new block position
			new.block.X = int8(x + dx)
			new.block.Y = int8(y + dy)

			// update pos
			new.pos.X = int8(x2)
			new.pos.Y = int8(y2)

			// add to the heap
			if _, seen := visited[new]; !seen {
				queue = append(queue, new)
			}
		}
	}

	for i := range safe {
		g.dead[i] = ^safe[i]
	}
}
