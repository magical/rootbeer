// Copyright 2020 Andrew Ekstedt
// This program is licensed under the GNU Affero General
// Public License v3.0. See LICENSE for details.

package main

import (
	"bytes"
	"testing"
)

func TestLevel(t *testing.T) {
	var level = &Level{
		Title:     "Test",
		TimeLimit: 500,
	}
	// draw a 10x10 border
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			if x == 0 || x == 9 || y == 0 || y == 9 {
				level.Tiles[y][x] = Wall
			}
		}
	}
	level.Tiles[1][1] = Player
	level.Tiles[1][2] = Block
	level.Tiles[1][3] = Exit
	level.Tiles[1][4] = Teleport

	var buf bytes.Buffer
	if err := saveLevel(&buf, level); err != nil {
		t.Errorf("error saving level: %v", err)
		return
	}

	if l, err := DecodeLevel(buf.Bytes()); err != nil {
		t.Errorf("error reading level: %v", err)
		if l != nil {
			t.Errorf("got non-nil level despite error during decode")
		}
	} else {
		if l == nil {
			t.Errorf("got level=nil with err=nil")
		}
	}

}
