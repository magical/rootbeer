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
	g.walls = Bitmap{
		0b1001000101,
		0b1000010001,
		0b1010000001,
		0b1000101011,
		0b1101000001,
		0b1000100101,
		0b1000000001,
		0b1001000101,
		0b1011111111,
		0b1111111111,
	}
	g.sink = Point{X: 8, Y: 8}
	g.startPos = Point{X: 8, Y: 7}
	g.progress = progress

	for i := range g.walls {
		g.walls[i] = bits.Reverse16(g.walls[i]) >> 6
	}
	g.sink.X = 1
	g.startPos.X = 1

	if *mapflag != "" {
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
				if level.Tiles[y][x] == Wall {
					g.walls.Set(int8(x), int8(y), true)
				}
				if level.Tiles[y][x] == Teleport {
					g.sink.X = int8(x)
					g.sink.Y = int8(y)
				}
				if level.Tiles[y][x] == Player {
					g.startPos.X = int8(x)
					g.startPos.Y = int8(y)
					foundPlayer = true
				}
				if level.Tiles[y][x] == Fire {
					g.fire.Set(int8(x), int8(y), true)
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
	if node.len < len(g.count) {
		fmt.Println("found", g.count[node.len], "solutions of length", node.len)
	}

	if *outflag != "" {
		var level Level
		level.Title = "Computer"
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if g.walls.At(int8(x), int8(y)) {
					level.Tiles[y][x] = Wall
				} else if g.fire.At(int8(x), int8(y)) {
					level.Tiles[y][x] = Fire
				}
				if node.state.blocks.At(int8(x), int8(y)) {
					level.push(x, y, Block)
				}
			}
		}
		level.push(int(node.state.pos.X), int(node.state.pos.Y), Player)
		level.Tiles[g.sink.Y][g.sink.X] = Teleport
		err := SaveLevel(*outflag, &level)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func (l *Level) push(x, y int, t Tile) {
	if l.Tiles[y][x] != Floor {
		l.Subtiles[y][x] = l.Tiles[y][x]
	}
	l.Tiles[y][x] = t
}

type Generator struct {
	walls    Bitmap
	sink     Point // where the block have to go (come from)
	startPos Point
	fire     Bitmap

	// areas where chip cannot go: walls+fire
	nogo Bitmap

	count    [256]int
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
	g.nogo = g.walls.Union(g.fire)
	var visited = make(map[state]struct{})
	var queue nodeQueue // []*node
	var start = new(node)
	var max = start
	start.state.blocks.Set(g.sink.X, g.sink.Y, true)
	start.state.pos = g.startPos
	start.state.normalize(&g.walls)
	log.Print("\n", formatLevel(g, start))
	queue = append(queue, start)
	for len(queue) > 0 {
		no := heap.Pop(&queue).(*node)
		if _, ok := visited[no.state]; ok {
			continue
		}
		visited[no.state] = struct{}{}
		if no.len < len(g.count) {
			g.count[no.len]++
		}

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

		// find reachable squares
		r := reachable(no.state.pos.X, no.state.pos.Y, &g.nogo, &no.state.blocks)

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

					// in order to pull,
					// two squares in the pull direction
					// must be reachable & clear
					if x+dx*2 < 0 || x+dx*2 >= width {
						continue
					}
					if y+dy*2 < 0 || y+dy*2 >= height {
						continue
					}
					if !r.At(int8(x+dx*2), int8(y+dy*2)) {
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
					new.state.blocks.Set(int8(x+dx), int8(y+dy), true)

					// there is always a block at the sink
					new.state.blocks.Set(g.sink.X, g.sink.Y, true)

					// update pos
					new.state.pos.X = int8(x + dx*2)
					new.state.pos.Y = int8(y + dy*2)

					new.state.normalize(&g.nogo)

					// add to the heap
					if _, ok := visited[new.state]; ok {
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

func (s *state) normalize(walls *Bitmap) {
	r := reachable(s.pos.X, s.pos.Y, &s.blocks, walls)
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
				if g.fire.At(int8(x), int8(y)) {
					s = append(s, "[&"...)
				} else {
					s = append(s, "[]"...)
				}
			} else if g.fire.At(int8(x), int8(y)) {
				s = append(s, "&&"...)
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
