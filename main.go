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
					g.button = append(g.button, p)
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
		g.walls[i] |= ^uint8(0) >> width << width
	}

	//fmt.Println(g.walls.String())
	node := g.Search()
	fmt.Println(node.len)
	fmt.Println(node.state.pos)
	for n := node; n != nil; n = n.parent {
		fmt.Print(formatLevel(&g, n))
		fmt.Println(n.state.nblocks())
		fmt.Println("-")
	}
	//pretty.Println(node.state)
	if node.len < len(g.count) {
		fmt.Println("found", g.count[node.len], "solutions of length", node.len)
	}

	if *outflag != "" {
		var level Level
		level.Title = "Computer"
		push := func(y, x int, t Tile) {
			if level.Tiles[y][x] != Floor {
				level.Subtiles[y][x] = level.Tiles[y][x]
			}
			level.Tiles[y][x] = t
		}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				closed := 0
				if g.isToggleFlipped(int8(x), int8(y), node.state.toggle) {
					closed ^= 1
				}
				if g.walls.At(int8(x), int8(y)) {
					level.Tiles[y][x] = Wall
				} else if g.toggle[closed].At(int8(x), int8(y)) {
					level.Tiles[y][x] = ToggleWall
				} else if g.toggle[closed^1].At(int8(x), int8(y)) {
					level.Tiles[y][x] = ToggleFloor
				} else if g.buttonAt(int8(x), int8(y)) >= 0 {
					level.Tiles[y][x] = ToggleButton
				}
				if node.state.blocks.At(int8(x), int8(y)) {
					push(y, x, Block)
				}
			}
		}
		push(int(node.state.pos.Y), int(node.state.pos.X), Player)
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
	button   []Point // toggle button pos

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

const maxBlocks = 100

// number of squares to consider during a block line
// if set to 1 this becomes the normal push metric
const maxPush = 1

func (g *Generator) Search() *node {
	var visited = make(map[state]struct{})
	var queue nodeQueue // []*node
	var blocks []Point
	var interactible Bitmap

	// init the states:
	// 2^n of them, one for each toggle state
	var nogo = new([256]Bitmap)
	for t := uint8(0); t < 1<<len(g.button); t++ {
		nogo[t] = g.walls
		g.applyToggles(&nogo[t], t)
	}

	// init the queue with one state for each toggle state
	for t := uint8(1); t < 1<<len(g.button); t++ {
		start := new(node)
		start.state.blocks.Set(g.sink.X, g.sink.Y, true)
		start.state.pos = g.startPos
		start.state.toggle = t
		start.state.normalize(&nogo[t])
		heap.Push(&queue, start)
		log.Print("\n", formatLevel(g, start))
	}

	for _, p := range g.button {
		interactible.Set(p.X, p.Y, true)
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

		// note: although the player might have to step on a button in order to
		// reach a square, they can always press it again (unless it traps them i guess)
		// in order to keep the walls the same, so we can ignore that when
		// considering which blocks are reachable

		// TODO:
		// - can flick blocks off toggle walls in MSCC

		// if we can't move, give up
		// (in particular, don't allow chip to press the button he's standing on)
		if !r.hasNeighbor(no.state.pos.X, no.state.pos.Y) {
			continue
		}

		// press toggle buttons
		for i, p := range g.button {
			if r.At(p.X, p.Y) {
				new := newnode()
				*new = node{
					state:  no.state,
					parent: no,
					len:    no.len + 1,
				}

				// flip the toggle walls
				new.state.toggle ^= 1 << uint(i)

				// update pos & normalize
				// it's fine to normalize, even though stepping on the button
				// changes state:
				// - either there's a free square adjacent, in which case we can press it again
				// - or there's not, in which case normalizing won't change anything
				new.state.pos = p
				new.state.normalize(&nogo[new.state.toggle])

				if _, haveVisited := visited[new.state]; !haveVisited {
					heap.Push(&queue, new)
				}

				// TODO: if any button traps us, block that square and recompute reachable
			}
		}

		// find blocks
		blocks = blocks[:0]
		for i, bl := range no.state.blocks {
			/// TODO: maybe mask bl with r?
			for bl != 0 {
				y := i
				x := bits.Len8(bl) - 1
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

				// the way pushing interacts with button presses,
				// 1) first we push the block
				// 2) then button presses apply
				// so when figuring out if we can pull,
				// we have to do it backwards: first work
				// out the new toggle state, then see if
				// anything is blocked under that new state

				newToggleState := no.state.toggle
				if interactible.At(int8(x), int8(y)) {
					j := g.buttonAt(int8(x), int8(y))
					newToggleState ^= 1 << uint(j)
				}
				if interactible.At(int8(x+dx), int8(y+dy)) {
					j := g.buttonAt(int8(x+dx), int8(y+dy))
					newToggleState ^= 1 << uint(j)
				}

				// block can't be on a toggle wall unless chip is on a button
				if nogo[newToggleState].At(int8(x), int8(y)) {
					continue
				}
				// chip can't be on a toggle wall unless the block is on a button
				if nogo[newToggleState].At(int8(x+dx), int8(y+dy)) {
					continue
				}

				{
					const j = 1
					// in order to pull,
					// (j+1) squares in the pull direction
					// must be reachable & clear
					if x+dx*(j+1) < 0 || x+dx*(j+1) >= width {
						continue
					}
					if y+dy*(j+1) < 0 || y+dy*(j+1) >= height {
						continue
					}
					// chips' destination tile must be reachable given the new toggle state
					if nogo[newToggleState].At(int8(x+dx*(j+1)), int8(y+dy*(j+1))) ||
						no.state.blocks.At(int8(x+dx*(j+1)), int8(y+dy*(j+1))) {
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
					if new.state.nblocks() < maxBlocks {
						new.state.blocks.Set(g.sink.X, g.sink.Y, true)
					}

					new.state.toggle = newToggleState

					//// if we end up on a toggle, activate it
					//// this can matter if we are trapped afterwards
					//if interactible.At(int8(x+dx*(j+1)), int8(y+dy*(j+1))) {
					//	k := g.buttonAt(int8(x+dx*(j+1)), int8(y+dy*(j+1)))
					//	new.state.toggle ^= 1 << uint(k)
					//}

					// update pos
					new.state.pos.X = int8(x + dx*(j+1))
					new.state.pos.Y = int8(y + dy*(j+1))

					new.state.normalize(&nogo[new.state.toggle])

					// add to the heap
					if _, haveVisited := visited[new.state]; !haveVisited {
						heap.Push(&queue, new)
					}
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

// apply area effect toggles to the state
// not fast
func (g *Generator) applyToggles(b *Bitmap, t uint8) {
	// initialize toggle state
	*b = b.Union(g.toggle[0])
	// iterate over each square
	for y := int8(0); y < height; y++ {
		for x := int8(0); x < width; x++ {
			// skip non-toggle wall/floor squares
			if !g.hasToggleTerrain(x, y) {
				continue
			}
			// flip the state at this cell if necessary
			if g.isToggleFlipped(x, y, t) {
				b.Set(x, y, !b.At(x, y))
			}
		}
	}
}

// reports whether the toggle wall or floor at x,y is flipped from its initial state given the current toggle state
func (g *Generator) isToggleFlipped(x, y int8, t uint8) bool {
	flipped := 0
	// iterate over active buttons
	for i, pos := range g.button {
		if t>>i&1 != 0 {
			// if we are within a 5x5 area around the button...
			if pos.X-2 <= x && x <= pos.X+2 &&
				pos.Y-2 <= y && y <= pos.Y+2 {
				// invert the toggle state at this cell
				flipped ^= 1
			}
		}
	}
	return flipped != 0
}

// returns -1 if not found
func (g *Generator) buttonAt(x, y int8) int {
	for i, p := range g.button {
		if p.X == x && p.Y == y {
			return i
		}
	}
	return -1
}

func (s *state) nblocks() int {
	n := 0
	for i := range &s.blocks {
		n += bits.OnesCount8(s.blocks[i])
	}
	return n
}

func (s *state) normalize(walls *Bitmap) {
	if walls.At(s.pos.X, s.pos.Y) {
		return
	}
	r := reachable(s.pos.X, s.pos.Y, &s.blocks, walls)
	for i := range r {
		if r[i] != 0 {
			s.pos.Y = int8(i)
			s.pos.X = int8(bits.Len8(r[i]) - 1)
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
		prev := uint8(0)
		changed := uint8(0)
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

// reports whether a tile adjecent to pos is in the bitmap
func (b *Bitmap) hasNeighbor(x, y int8) bool {
	m := uint8(1) << uint(x)
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
			closed := 0
			if g.isToggleFlipped(int8(x), int8(y), n.state.toggle) {
				closed ^= 1
			}
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

func (g *Generator) hasToggleTerrain(x, y int8) bool {
	return g.toggle[0].At(x, y) || g.toggle[1].At(x, y)
}
