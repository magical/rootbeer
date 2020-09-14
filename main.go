package main

import (
	"container/heap"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/bits"
	"os"
	"runtime"
	"time"
)

func main() {
	mapflag := flag.String("map", "", "levelset to load walls from [required]")
	outflag := flag.String("o", "", "file to save generated level to [optional]")
	progressflag := flag.Bool("progress", false, "show progress")
	flag.Parse()

	var progress <-chan time.Time
	if *progressflag {
		progress = time.Tick(1 * time.Second)
	}

	var g Generator
	g.progress = progress

	if *mapflag == "" {
		fmt.Fprintln(os.Stderr, "error: -map flag is required")
		os.Exit(1)
	} else {
		data, err := ioutil.ReadFile(*mapflag)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		level, err := DecodeLevel(data)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		g.walls = Bitmap{}
		foundPlayer := false
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				switch level.Tiles[y][x] {
				case Wall:
					g.walls.Set(int8(x), int8(y), true)
				case Teleport:
					g.sink.X = int8(x)
					g.sink.Y = int8(y)
				case Player:
					g.startPos.X = int8(x)
					g.startPos.Y = int8(y)
					foundPlayer = true
				case Trap:
					g.trap.X = int8(x)
					g.trap.Y = int8(y)
				case TrapButton:
					g.button.X = int8(x)
					g.button.Y = int8(y)
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

	for i := range g.walls {
		g.walls[i] |= 0xFFFF >> width << width
	}

	//fmt.Println(g.walls.String())
	node := g.Search()
	fmt.Println(node.len)
	fmt.Println(node.state.pos)
	for n := node; n != nil; n = n.parent {
		fmt.Print(formatLevel(&g, n))
		fmt.Println("-")
	}
	//pretty.Println(node.state)

	if *outflag != "" {
		var level Level
		level.Title = "Computer"
		// TODO: set buttom layer if necessary
		// TODO: connect button to trap
		level.Tiles[g.trap.Y][g.trap.X] = Trap
		level.Tiles[g.button.Y][g.button.X] = TrapButton
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if g.walls.At(int8(x), int8(y)) {
					level.Tiles[y][x] = Wall
				} else if node.state.blocks.At(int8(x), int8(y)) {
					level.Tiles[y][x] = Block
				}
			}
		}
		level.Tiles[node.state.pos.Y][node.state.pos.X] = Player
		level.Tiles[g.sink.Y][g.sink.X] = Teleport
		err := SaveLevel(*outflag, &level)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

}

type Generator struct {
	walls    Bitmap
	sink     Point // where the block have to go (come from)
	startPos Point

	trap   Point
	button Point

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

// TODO: don't assume there's always a trap

func (g *Generator) Search() *node {
	var visited = make(map[state]struct{})
	var queue nodeQueue // []*node
	var start = new(node)
	var traps Bitmap
	var newtraps Bitmap
	var max = start
	traps.Set(g.trap.X, g.trap.Y, true) // so starts off closed b/c no blocks
	start.state.blocks.Set(g.sink.X, g.sink.Y, true)
	start.state.pos = g.startPos
	start.state.normalize(&g.walls, &traps)
	log.Print("\n", formatLevel(g, start))
	queue = append(queue, start)
	for len(queue) > 0 {
		no := heap.Pop(&queue).(*node)
		if _, ok := visited[no.state]; ok {
			continue
		}
		visited[no.state] = struct{}{}

		if no.len > max.len {
			max = no
		}

		select {
		case <-g.progress:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			log.Printf("alloc: current %d MB, max %d MB, sys %d MB", m.Alloc/1e6, m.TotalAlloc/1e6, m.Sys/1e6)
			log.Printf("search: current %d, max %d, visited: %d, queue %d\n%s", no.len, max.len, len(visited), len(queue),
				no.state.blocks.String())
		default:
		}

		// the trap is closed if and only if there is no block on the button
		traps.Set(g.trap.X, g.trap.Y, !no.state.blocks.At(g.button.X, g.button.Y))

		// find reachable squares
		r := reachable(no.state.pos.X, no.state.pos.Y, &g.walls, &no.state.blocks, &traps)

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

				if traps.At(int8(x), int8(y)) {
					// trap is closed, so block cannot move
					// TODO: could mask bl instead
					continue
				}

				for _, d := range dirs {
					dx, dy := int(d.X), int(d.Y)

					// square beside the block must be
					// reachable and not blocked
					if x+dx < 0 || x+dx >= width {
						continue
					}
					if y+dy < 0 || y+dy >= height {
						continue
					}
					if !r.At(int8(x+dx), int8(y+dy)) {
						continue
					}

					// block lines metric:
					// pulling a block multiple squares in one direction
					// counts as a single move
					for j := 1; j < 30; j++ {
						// in order to pull,
						// (j+1) squares in the pull direction
						// must be reachable & clear
						if x+dx*(j+1) < 0 || x+dx*(j+1) >= width {
							break
						}
						if y+dy*(j+1) < 0 || y+dy*(j+1) >= height {
							break
						}
						if !r.At(int8(x+dx*(j+1)), int8(y+dy*(j+1))) {
							break
						}

						new := newnode()
						*new = node{
							state:  no.state,
							parent: no,
							len:    no.len + 1,
						}
						// set the new block position
						new.state.blocks.Set(int8(x), int8(y), false)
						new.state.blocks.Set(int8(x+dx*j), int8(y+dy*j), true)

						// there is always a block at the sink
						new.state.blocks.Set(g.sink.X, g.sink.Y, true)

						// update pos
						new.state.pos.X = int8(x + dx*(j+1))
						new.state.pos.Y = int8(y + dy*(j+1))

						// the trap is closed if and only if there is no block on the button
						newtraps.Set(g.trap.X, g.trap.Y, !new.state.blocks.At(g.button.X, g.button.Y))

						new.state.normalize(&g.walls, &newtraps)

						// add to the heap
						if _, ok := visited[new.state]; ok {
							continue
						}
						heap.Push(&queue, new)
					}
				}
			}
		}
	}
	log.Println("visited ", len(visited), "states")
	return max
}

func (s *state) normalize(walls, extra *Bitmap) {
	r := reachable(s.pos.X, s.pos.Y, &s.blocks, walls, extra)
	for i := range r {
		if r[i] != 0 {
			s.pos.Y = int8(i)
			s.pos.X = int8(bits.Len16(r[i]) - 1)
			return
		}
	}
}

// Return a bitmap of all squares reachable from x,y
// without visiting mask1 or mask2
func reachable(x, y int8, mask1, mask2, mask3 *Bitmap) Bitmap {
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
			tmp2 &^= mask3[i]
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

var nodepool []node

// bump allocator for nodes
func newnode() *node {
	if len(nodepool) == 0 {
		nodepool = make([]node, 100000)
	}
	node := &nodepool[0]
	nodepool = nodepool[1:]
	return node
}

func formatLevel(g *Generator, n *node) string {
	var s []byte
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if g.walls.At(int8(x), int8(y)) {
				s = append(s, "##"...)
			} else if n.state.blocks.At(int8(x), int8(y)) {
				s = append(s, "[]"...)
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
