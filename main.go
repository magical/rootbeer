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

					// BLOCK SLAPPING
					// in the Lynx and CC2 rulesets, you can slap blocks while moving
					// by pressing two directions at once. the player continues moving
					// but pushes a block to the side while doing so.
					//
					// it is possible to slap from a standstill. press both buttons at once:
					// the player will move in the direction they are facing.
					//
					// in TW Lynx, it is only possible to slap when moving in the facing direction.
					// In CC2, it is also possible when turning around (opposite of facing direction).
					// the latter is simpler to support
					//
					// for simplicity, we only consider slapping blocks that are on a fire tile.
					// (otherwise a normal push suffices)
					//
					// the generator runs the game in reverse, which makes implementing block slapping a lot harder.
					// instead of implementing it as a single move which does two things, we split the move into two:
					// *first* the player slaps the block, while staying motionless,
					// *then* they move,
					// except the other way around because we're doing this in reverse.
					// That is, when considering whether to slap a block we have to assume
					// that the movement that would come after it has already happened.
					// That is, the previous state is the result of the move and this state is the preceding slap.
					// so we just have to figure out based on the current state if the player could have just
					// moved in a way that is compatible with a slap.
					//
					// to wit:
					// suppose we are considering whether to (anti-)slap a block
					// from the following position:
					//
					//        n             n
					//  [] w  @ e  -> _ [w] @ e
					//        s             s
					//  0  1  2       0  1  2
					//
					// a slap is only possible if we just exited position 2 from n or s
					// *and* if we could be facing n or s before the slap.
					// we can ignore e entirely.
					//
					// this implies:
					// 1. n or s must be reachable or be occupied by a block
					// 2. if n and s are empty we can always slap, by moving from n to s
					// 3. if n or s is empty we can slap, by entering from that direction and doing a turnaround slap
					// 4. if one of n or s is blocked by a wall (or two blocks) and the other is emty,
					//    we can slap by bumping into the wall to set facing and then exiting in the other direction
					//    (even if we came from e)
					// 5. if both n or s is blocked by a movable block, we could theoretically slap while moving that block away
					//    but to implement that we'd have to verify that the current state moves that block, which is hard.
					//    however! if we do (did) move that block out of the way then this becomes case (2) or (3), so
					//    we already had the option to slap so we don't need to handle it now
					// 6. if n and s are both blocked then slapping is impossible
					//
					// note that it is impossible to slap two directions at once,
					// but in all the above cases it is possible to leave the tile and come
					// back to slap a second time, so we don't care.
					//
					// this gets extremely more complicated with popup walls and other one-shot tiles
					// but fortunately we don't have to deal with that yet!

					// only consider slaps of blocks on a fire tile
					// otherwise a regular push suffices
					if g.fire.At(int8(x+dx), int8(y+dy)) {
						if x+dx*2 < 0 || x+dx*2 >= width ||
							y+dy*2 < 0 || y+dy*2 >= height ||
							!r.At(int8(x+dx*2), int8(y+dy*2)) {
							continue
						}
						// per the big comment above, we can slap as long as one of the perpendicular directions is empty
						for _, p := range [2]Point{
							{X: int8((x + dx*2) + dy), Y: int8(y + dy*2 + dx)},
							{X: int8((x + dx*2) - dy), Y: int8(y + dy*2 - dx)},
						} {
							if r.At(p.X, p.Y) {
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
								// XXX but my level don't have fire right next to the sink
								//new.state.blocks.Set(g.sink.X, g.sink.Y, true)

								// renormalize pos
								new.state.pos.X = int8(x + dx*2)
								new.state.pos.Y = int8(y + dy*2)
								new.state.normalize(&g.nogo)

								// add to the heap
								if _, seen := visited[new.state]; seen {
									heap.Push(&queue, new)
								}

								// don't bother checking the other direction.
								// only need one to slap
								break
							}
						}
					} else if !r.At(int8(x+dx), int8(y+dy)) {
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
