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
	mapflag := flag.String("map", "", "levelset to load walls from [optional]")
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
				p := Point{X: int8(x), Y: int8(y)}
				switch level.Tiles[y][x] {
				case Wall:
					g.walls.Set(p.X, p.Y, true)
				case Teleport:
					g.sink = p
				case Player:
					g.startPos = p
					foundPlayer = true
				case Dirt:
					g.dirt = append(g.dirt, DirtPoint{p, Dirt})
				case Popup:
					g.dirt = append(g.dirt, DirtPoint{p, Popup})
					g.walls.Set(p.X, p.Y, true)
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
	fmt.Println(g.dirt)
	node := g.Search()
	fmt.Println(node.len)
	fmt.Println(node.state.pos)
	for n := node; n != nil; n = n.parent {
		fmt.Print(formatLevel(&g, n))
		r := reachable(n.state.pos.X, n.state.pos.Y, &g.walls, &n.state.blocks, &n.state.active)
		fmt.Print(r.String())
		fmt.Println("-")
	}
	//pretty.Println(node.state)

	if *outflag != "" {
		var level Level
		level.Title = "Computer"
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if g.walls.At(int8(x), int8(y)) && !node.state.active.At(int8(x), int8(y)) {
					level.Tiles[y][x] = Wall
				} else if node.state.blocks.At(int8(x), int8(y)) {
					level.Tiles[y][x] = Block
				}
			}
		}
		level.Tiles[node.state.pos.Y][node.state.pos.X] = Player
		level.Tiles[g.sink.Y][g.sink.X] = Teleport
		for _, d := range g.dirt {
			if node.state.active.At(d.pos.X, d.pos.Y) {
				if level.Tiles[d.pos.Y][d.pos.X] == Floor {
					level.Tiles[d.pos.Y][d.pos.X] = d.tile
				} else {
					log.Printf("warning: tile %d,%d should be %d but is occupied", d.pos.X, d.pos.Y, d.tile)
				}
			}
		}
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
	dirt     []DirtPoint
	popups   Bitmap

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

// Dirt & Popup walls:
// - dirt starts off as a floor; once activted it becomes a wall
// - popup walls start off as walls; once activated they become walls
//
// why do these tiles become walls once activated?
// because they can only be stepped on once,
// so once activated we have to avoid stepping on them.
//
// activated tiles are kept track of in state.active
// state.active is added to the set of walls in reachable
// popup walls are also added to g.walls since they are always treated as walls

type DirtPoint struct {
	pos  Point
	tile Tile // Dirt or Popup
}

type Point struct{ Y, X int8 }

var dirs = [4]Point{
	{-1, 0}, {+1, 0}, {0, -1}, {0, +1},
}

func (g *Generator) Search() *node {
	var visited = make(map[state]struct{})
	var queue nodeQueue // []*node
	var start = new(node)
	var max = start
	start.state.blocks.Set(g.sink.X, g.sink.Y, true)
	start.state.pos = g.startPos
	start.state.normalize(&g.walls)
	log.Print("\n", formatLevel(g, start))
	for _, d := range g.dirt {
		if d.tile == Popup {
			g.popups.Set(d.pos.X, d.pos.Y, true)
		}
	}
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
			log.Printf("search: current %d, max %d, visited: %d, queue %d\n%s%s", no.len, max.len, len(visited), len(queue),
				no.state.blocks.String(), no.state.active.String())
		default:
		}

		// find reachable squares
		r := reachable(no.state.pos.X, no.state.pos.Y, &g.walls, &no.state.blocks, &no.state.active)

		// activate dirt / popup walls
		for _, d := range g.dirt {
			var canActivate = false
			if d.tile == Dirt {
				if r.At(d.pos.X, d.pos.Y) && !no.state.active.At(d.pos.X, d.pos.Y) {
					canActivate = true
				}
			} else if d.tile == Popup {
				if !no.state.active.At(d.pos.X, d.pos.Y) && r.hasNeighbor(d.pos.X, d.pos.Y) {
					canActivate = true
				}
			}
			if canActivate {
				new := newnode()
				*new = node{
					state:  no.state,
					parent: no,
					len:    no.len + 1,
				}
				new.state.pos = d.pos
				new.state.active.Set(d.pos.X, d.pos.Y, true)

				// add to the heap
				if _, ok := visited[new.state]; ok {
					continue
				}
				heap.Push(&queue, new)
			}
		}

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
					// can't pull blocks onto an active square
					if no.state.active.At(int8(x+dx), int8(y+dy)) {
						continue
					}

					// we *can* pull onto an inactive popup walll
					// [] @  o     _  [](@)
					// 0  1  2  -> 0  1  2
					if 0 <= x+dx+dx && x+dx+dx < width && 0 <= y+dy+dy && y+dy+dy < height &&
						g.popups.At(int8(x+dx+dx), int8(y+dy+dy)) &&
						!no.state.active.At(int8(x+dx+dx), int8(y+dy+dy)) {

						new := newnode()
						*new = node{
							state:  no.state,
							parent: no,
							len:    no.len + 1,
						}
						// set the new block position
						new.state.blocks.Set(int8(x), int8(y), false)
						new.state.blocks.Set(int8(x+dx), int8(y+dy), true)

						// there is always a block at the sink
						new.state.blocks.Set(g.sink.X, g.sink.Y, true)

						// activate popup
						new.state.active.Set(int8(x+dx+dx), int8(y+dy+dy), true)

						// update pos
						new.state.pos.X = int8(x + dx + dx)
						new.state.pos.Y = int8(y + dy + dy)

						// add to the heap
						if _, seen := visited[new.state]; !seen {
							heap.Push(&queue, new)
						}
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
						// can't pull blocks onto an active square
						if no.state.active.At(int8(x+dx*(j+1)), int8(y+dy*(j+1))) {
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
						new.state.blocks.Set(int8(x+dx*j), int8(y+dy*j), true)

						// there is always a block at the sink
						new.state.blocks.Set(g.sink.X, g.sink.Y, true)

						// update pos
						new.state.pos.X = int8(x + dx*(j+1))
						new.state.pos.Y = int8(y + dy*(j+1))

						new.state.normalize(&g.walls)

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

func (s *state) normalize(walls *Bitmap) {
	r := reachable(s.pos.X, s.pos.Y, &s.blocks, walls, &s.active)
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

// reports whether a tile adjecent to pos is in the bitmap
func (b *Bitmap) hasNeighbor(x, y int8) bool {
	m := uint16(1) << uint(x)
	if b[y]&((m<<1)|(m>>1)) != 0 {
		return true
	}
	if y > 0 && b[y-1]&m != 0 {
		return true
	}
	if int(y)+1 < len(b) && b[y+1]&m != 0 {
		return true
	}
	return false
}

type node struct {
	state  state
	parent *node
	len    int
}

type state struct {
	blocks Bitmap
	// active keeps track of which popup walls / dirt tiles have been activated
	// once activated they are treated as a wall
	active Bitmap
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
			if g.walls.At(int8(x), int8(y)) && !n.state.active.At(int8(x), int8(y)) {
				s = append(s, "##"...)
			} else if n.state.active.At(int8(x), int8(y)) {
				if x == int(n.state.pos.X) && y == int(n.state.pos.Y) {
					s = append(s, "$/"...)
				} else if n.state.blocks.At(int8(x), int8(y)) {
					s = append(s, "[/"...)
				} else {
					s = append(s, "//"...)
				}
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
