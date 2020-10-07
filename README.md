Root Beer Generator, aka `rootbeer`, aka The Notorious R.B.G. is a generator for sokoban-like puzzles.

The generator works by running a level backwards, pulling blocks from their final positions to the starting positions,
meaning it only ever deals with solvable game states.
You give it the walls of the level and it finds the longest solution.
