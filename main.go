// Copyright 2020 Andrew Ekstedt
// This program is licensed under the GNU Affero General
// Public License v3.0. See LICENSE for details.

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

	type Bitmap = Bitmap8
	var height = len(new(Bitmap))
	var g Generator[Bitmap, *Bitmap]
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
					g.walls.Set(int8(x), int8(y), true)
				case Teleport:
					g.sink = p
				case Player:
					g.startPos = p
					foundPlayer = true
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

	for i := range g.walls.Slice() {
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

type Generator[Bitmap BitmapT[Bitmap], BitmapP BitmapI[Bitmap]] struct {
	// TODO: add a separate field for unenterable tiles
	// and let walls just be walls
	walls    Bitmap
	sink     Point // where the block have to go (come from)
	startPos Point

	nodepool []node[Bitmap, BitmapP]
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

// number of squares to consider during a block line
// if set to 1 this becomes the normal push metric
const maxPush = 1

func (g *Generator[Bitmap, BitmapP]) Search() *node[Bitmap, BitmapP] {
	var height = len(*new(Bitmap))
	var visited = make(map[state[Bitmap, BitmapP]]struct{})
	var queue nodeQueue[Bitmap, BitmapP] // []*node
	var start = new(node[Bitmap, BitmapP])
	var max = start
	var blocks []Point
	BitmapP(&start.state.blocks).Set(g.sink.X, g.sink.Y, true)
	start.state.pos = g.startPos
	start.state.normalize(&g.walls)
	log.Print("\n", formatLevel(g, start))
	queue = append(queue, start)
	for len(queue) > 0 {
		no := heap.Pop(&queue).(*node[Bitmap, BitmapP])
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
				BitmapP(&no.state.blocks).String())
		default:
		}

		// find reachable squares
		var r Bitmap
		BitmapP(&r).reachable(no.state.pos.X, no.state.pos.Y, &g.walls, &no.state.blocks)

		// find blocks
		blocks = blocks[:0]
		for i, bl := range BitmapP(&no.state.blocks).Slice() {
			/// TODO: maybe mask bl with r?
			for bl != 0 {
				y := i
				x := bits.Len16(bl) - 1
				bl = bl & ((1 << x) - 1) // clear current block
				if !BitmapP(&no.state.blocks).At(int8(x), int8(y)) {
					panic("block does not exist")
				}
				blocks = append(blocks, Point{X: int8(x), Y: int8(y)})
			}
		}

		// iterate over each block
		// and find valid moves
		for _, p := range blocks {
			x, y := int(p.X), int(p.Y)
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
				if !BitmapP(&r).At(int8(x+dx), int8(y+dy)) {
					continue
				}

				// block lines metric:
				// pulling a block multiple squares in one direction
				// counts as a single move
				for j := 1; j < maxPush+1; j++ {
					// in order to pull,
					// (j+1) squares in the pull direction
					// must be reachable & clear
					if x+dx*(j+1) < 0 || x+dx*(j+1) >= width {
						break
					}
					if y+dy*(j+1) < 0 || y+dy*(j+1) >= height {
						break
					}
					if !BitmapP(&r).At(int8(x+dx*(j+1)), int8(y+dy*(j+1))) {
						break
					}

					new := g.newnode()
					*new = node[Bitmap, BitmapP]{
						state:  no.state,
						parent: no,
						len:    no.len + 1,
					}
					// set the new block position
					BitmapP(&new.state.blocks).Set(int8(x), int8(y), false)
					BitmapP(&new.state.blocks).Set(int8(x+dx*j), int8(y+dy*j), true)

					// there is always a block at the sink
					BitmapP(&new.state.blocks).Set(g.sink.X, g.sink.Y, true)

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
	log.Println("visited ", len(visited), "states")
	return max
}

func (s *state[Bitmap, BitmapP]) normalize(walls BitmapP) {
	var r Bitmap
	BitmapP(&r).reachable(s.pos.X, s.pos.Y, &s.blocks, (*Bitmap)(walls))
	for i := range BitmapP(&r).Slice() {
		if r[i] != 0 {
			s.pos.Y = int8(i)
			s.pos.X = int8(bits.Len16(r[i]) - 1)
			return
		}
	}
}

// Sets b to the bitmap of all squares reachable from x,y
// without visiting mask1 or mask2
func (b *Bitmap8) reachable(x, y int8, mask1, mask2 *Bitmap8) {
	var a Bitmap8
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
	*b = a
}

func (b *Bitmap5) reachable(x, y int8, mask1, mask2 *Bitmap5) {
	var a Bitmap5
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
	*b = a
}

type node[Bitmap BitmapT[Bitmap], BitmapP BitmapI[Bitmap]] struct {
	state  state[Bitmap, BitmapP]
	parent *node[Bitmap, BitmapP]
	len    int
}

type state[Bitmap BitmapT[Bitmap], BitmapP BitmapI[Bitmap]] struct {
	blocks Bitmap
	pos    Point // position of player after last pull
}

type nodeQueue[T BitmapT[T], PT BitmapI[T]] []*(node[T, PT])

func (h nodeQueue[T, PT]) Len() int            { return len(h) }
func (h nodeQueue[T, PT]) Less(i, j int) bool  { return h[i].len < h[j].len }
func (h nodeQueue[T, PT]) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *nodeQueue[T, PT]) Push(x interface{}) { *h = append(*h, x.(*node[T, PT])) }
func (h *nodeQueue[T, PT]) Pop() interface{} {
	x := (*h)[len(*h)-1]
	*h = (*h)[:len(*h)-1]
	return x
}

// bump allocator for nodes
func (g *Generator[Bitmap, BitmapP]) newnode() *node[Bitmap, BitmapP] {
	if len(g.nodepool) == 0 {
		g.nodepool = make([]node[Bitmap, BitmapP], 100000)
	}
	node := &g.nodepool[0]
	g.nodepool = g.nodepool[1:]
	return node
}

func formatLevel[Bitmap BitmapT[Bitmap], BitmapP BitmapI[Bitmap]](g *Generator[Bitmap, BitmapP], n *node[Bitmap, BitmapP]) string {
	var s []byte
	var height = len(*new(Bitmap))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if BitmapP(&g.walls).At(int8(x), int8(y)) {
				s = append(s, "##"...)
			} else if BitmapP(&n.state.blocks).At(int8(x), int8(y)) {
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
