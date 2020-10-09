What is this?
=============

Root Beer Generator, aka `rootbeer`, aka The Notorious R.B.G. is a generator for sokoban-like puzzles.

The generator works by running a level backwards, pulling blocks from their final positions to the starting positions,
meaning it only ever deals with solvable game states.
You give it the walls of the level and it finds the longest solution.

RGB was designed with Chip's Challenge in mind, although it could probably be adapted to other games.

It can read input levels in CCL format and write output in CCL format as well.

Usage & Installation
----

    go build -o rbg .
    ./rbg -map example1.ccl -o example1_out.ccl -progress

_- or -_

    go run . -map input.ccl -o output.ccl -progress


The `-progress` flag causes `rbg` to periodically log messages showing the progress of the search.

> 2015/10/21 19:28:00 alloc: current 682 MB, max 783 MB, sys 764 MB
> 2015/10/21 19:28:00 search: current 42, max 42, visited: 1959159, queue 814745

The `alloc` line tells you how much memory is currently allocated from the system (sys), how much we are using (cur), and the cumulative total we have ever allocated (max). Units are SI.
The `search` line tells you the length of the longest path found so far (max), the length of the path to the state we are currently visiting (cur, always the same as max), the total number of states visited so far (visited), and the number of states waiting to be visited (queue). The search does not stop until the queue is empty.


Usage (example)
----

> TODO: show an example level

<img src="example1.png" alt="[example level]">

Here's an example 6x6 level. It contains walls, and a teleport.

    go build -o rbg .
    ./rbg -map example1.ccl -o example1_out.ccl -progress

_- or -_

    go run . -map input.ccl -o output.ccl -progress

> TODO: show output

The output is not playable yet. We have to add an exit and provide a place for the blocks to go.
> TODO: screenshot of finished level
> TODO: screenshot w/ bombs
> TODO: screenshot w/ traps

Output:

The program starts by showing the level in a simple textual format

    2020/10/08 21:39:50
    . . . ##$ []##. .
    ## , , , , ,## , ,
    . . . ##. . ##. .
     , ,## , ,#### , ,
    . . . . . . ##. .
     , ,## , , ,## , ,
    ##############. .
     , , , , , , , , ,

When it is finished, it logs the number of states visited, and prints the
length of the solution it found, chip's starting position, and a textual
representation of the final state (the starting block configuration).
Followed by the state at each step in the solution.

Lastly, it prints the length of the optimal solution again and the number of paths it found of that length.

    2020/10/08 21:39:50 visited 1122 states
    39
    {2 5}
    . . . ##. []##. .
    ## , , ,[][]## , ,
    . . . ##. $ ##. .
     , ,## ,[]#### , ,
    . . . []. . ##. .
     , ,## , , ,## , ,
    ##############. .
     , , , , , , , , ,
    -
    [solution omitted]
    -
    found 3 solutions of length 39


In this case, RBG visited 1122 and found 3 that were 39 moves away from the input state.
That is, it found 3 ways to place the blocks (and player) such that the optimal solution requires 39 pushes.
One of these ways is shown, along with the solution.

Whether or not there are multiple Sometimes (pretty often) there are multiple maximal paths: different ways to place the starting blocks that, nevertheless, happen to have solutions of the same length.
RBG only shows one of these (arbitrarily, the first one that the search visits).
It would be possible to show the other paths too, but there is currently no user-configurable way to do so.

You can see the blocks in the ASCII art, represented by `[]` characters.


Note: this number is *not* the number of solutions for the generated level.
There may be many valid variations on the solution that RBG shows.
RBG makes no attempt to track all the possible variations of a solution.
The only thing we promise is that there is no shorter solution.

<img src="example1.png" alt="[same example level, but with blocks added]">

Input format
-----

> TODO: CCL, upper left corner, valid tiles (teleport!), border
> CCEdit is a good level editor

The Zen of RBG
--------------

> TODO

Other tiles
-----------

Support for other puzzle elements is in various branches of the project.
This was done to make it easy to experiment, because some puzzle elements are incompatible with each other, and because the interations between elements add significant complexity in some cases.
Some of these branches will probably be merged into the main branch at some point.

* `toggle` - Green toggle walls

* `thin` - Thin walls

* `gray` - Gray buttons and toggle walls. Gray buttons flip toggle walls in a 5x5 area around them when stepped on.

* `turtles` - Popup walls, turtles, and dirt. Popup walls can only be stepped on once.
  Turtles are like popup walls except you can also push blocks onto them.
  Dirt blocks blocks until cleared away by the player.

* `fire` - Fire and [block slapping][]

* `traps` - Single brown button and trap. Note: does not support trap ejection.

Note that the toggle walls and thin walls branches do not account for [flicking][] (pushing a block off of a wall).
Please design your levels accordingly.


Thanks to...
-----

- The Bit Busters Club, for providing encouragement and playtesting
- pieguy, for creating the original computer-generated level set and dropping some hints in chat about how it worked
