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
				p := Point{X: int8(x), Y: int8(y)}
				switch level.Tiles[y][x] {
				case Wall:
					g.walls.Set(p.X, p.Y, true)
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
	// TODO: add a separate field for unenterable tiles
	// and let walls just be walls
	walls    Bitmap
	toggle   [2]Bitmap // toggle walls/floors
	sink     Point     // where the block have to go (come from)
	startPos Point
	button   Point // toggle button pos

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

const maxBlocks = 8

// number of squares to consider during a block line
// if set to 1 this becomes the normal push metric
const maxPush = 1

func (g *Generator) Search() *node {
	var nogo [2]Bitmap
	nogo[0] = g.walls.Union(g.toggle[0])
	nogo[1] = g.walls.Union(g.toggle[1])

	var visited = make(map[state]struct{})
	var queue nodeQueue // []*node
	var blocks []Point

	// init the queue with two start states:
	// one for each toggle state
	for t := uint8(0); t < 2; t++ {
		start := new(node)
		start.state.blocks.Set(g.sink.X, g.sink.Y, true)
		start.state.pos = g.startPos
		start.state.toggle = t
		start.state.normalize(&nogo[t])
		heap.Push(&queue, start)
		log.Print("\n", formatLevel(g, start))
	}

	// GRAY BUTTONS
	// these are the area effect buttons from CC2 -
	// when you press one, tiles in two squares in each direction are toggled.
	// we can think of this as each button toggling a 5x5 invert field on and off.
	// when two buttons are active, the overlapping region cancels out. so the
	// field acts like a XOR operation. because XOR is associative, it doesn't
	// matter in which order the buttons are pressed, so we don't have to keep
	// track of the history of presses, just which buttons are active at the moment.
	//
	// because gray buttons are CC2 only, we don't have to worry about flicking
	// blocks off toggle walls and we don't have to avoid having blocks start
	// off atop toggle walls/floors.
	//
	// we *do* have to worry about blocks pressing buttons during a push,
	// or about buttons being in the path to a block, since we can't just
	// stick them in the corner of the level like we can with the green button.

	// :: = toggle floor
	// %% = toggle wall
	// o  = button
	// [] = block
	// @  = player
	//
	// [%] @  _              - can't pull block, since we would be pushing onto a wall
	// [:] @  _  => :: []  @ - can pull block
	// [%] @o _  => :: [o] @ - can pull, button swaps walls after push
	// [:] @o _              - can't pull, impossible
	// [%] @  o              - same as first case
	// [:] @  o  => :: [] @o - same as second case, doesn't activate toggle
	//
	// []  @: _   - can pull
	// []  @% _   - can't pull, block would've had to start on a wall but we can't push off a wall
	// [o] @% _   - can pull, activates toggle
	// [o] @: _   - can't pull
	//
	// toggle buttons only take effect when you (or a block) enters the tile.
	// button pushes take effect after block pushes.
	//
	// so to flip that around, when pulling a block,
	// - button pushes happen when chip or a block _leaves_ the button tile
	// - button pushes take effect _before_ block pushes
	//
	// also when chip ends a move on a wall or a button i guess we'll have to leave him there

	var max []*node
	for len(queue) > 0 {
		no := heap.Pop(&queue).(*node)
		if _, ok := visited[no.state]; ok {
			continue
		}
		visited[no.state] = struct{}{}
		if no.len < len(g.count) {
			g.count[no.len]++
		}

		if max == nil || no.len >= max[0].len {
			if max != nil && no.len > max[0].len {
				max = max[:0]
			}
			max = append(max, no)
		}

		select {
		case <-g.progress:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			log.Printf("alloc: current %d MB, max %d MB, sys %d MB", m.Alloc/1e6, m.TotalAlloc/1e6, m.Sys/1e6)
			log.Printf("search: current %d, max %d, visited: %d, queue %d\n%s", no.len, max[0].len, len(visited), len(queue),
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

		// find blocks
		blocks = blocks[:0]
		for i, bl := range no.state.blocks {
			/// TODO: maybe mask bl with r?
			for bl != 0 {
				y := i
				x := bits.Len16(bl) - 1
				bl = bl & ((1 << x) - 1) // clear current block
				if !no.state.blocks.At(int8(x), int8(y)) {
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
				if !r.At(int8(x+dx), int8(y+dy)) {
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
					if new.state.nblocks() < maxBlocks {
						new.state.blocks.Set(g.sink.X, g.sink.Y, true)
					}

					// update pos
					new.state.pos.X = int8(x + dx*(j+1))
					new.state.pos.Y = int8(y + dy*(j+1))

					new.state.normalize(&nogo[new.state.toggle])

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
	for _, no := range max {
		fmt.Println(formatLevel(g, no))
	}
	return max[0]
}

func (s *state) nblocks() int {
	n := 0
	for i := range &s.blocks {
		n += bits.OnesCount16(s.blocks[i])
	}
	return n
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
	toggle uint8 // state of toggle walls (0 or 1, corresponds to g.toggle)
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
