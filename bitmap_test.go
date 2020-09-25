package main

import "testing"

var reachableTests = []struct {
	pos       Point
	mask      Bitmap
	reachable Bitmap
}{
	{
		Point{X: 1, Y: 1},
		Bitmap{
			0b11111111,
			0b10000001,
			0b10000001,
			0b10000001,
			0b11111111,
		},
		Bitmap{
			0b00000000,
			0b01111110,
			0b01111110,
			0b01111110,
			0b00000000,
		},
	},
	{
		Point{X: 1, Y: 1},
		Bitmap{
			0b10100011,
			0b10101001,
			0b10101101,
			0b10001001,
			0b11111111,
		},
		Bitmap{
			0b01011100,
			0b01010110,
			0b01010010,
			0b01110110,
			0b00000000,
		},
	}}

func TestReachable(t *testing.T) {
	var zero Bitmap
	for _, tt := range reachableTests {
		mask := tt.mask
		r := reachable(tt.pos.X, tt.pos.Y, &mask, &zero)
		if r != tt.reachable {
			t.Errorf("\n%s\nwant:\n%s\ngot:\n%s", &tt.mask, &tt.reachable, &r)
		}
	}
}

var reachableThinTests = []struct {
	pos       Point
	mask      Bitmap
	thin      *thinspec
	reachable Bitmap
}{
	{
		Point{X: 1, Y: 1},
		Bitmap{
			0b11111,
			0b10001,
			0b10001,
			0b10001,
			0b11111,
		},
		// Single square in the middle with thin walls on all 4 inner sides
		&thinspec{
			N: Bitmap{0, 0, 0b00100, 0, 0},
			S: Bitmap{0, 0, 0b00100, 0, 0},
			E: Bitmap{0, 0, 0b00100, 0, 0},
			W: Bitmap{0, 0, 0b00100, 0, 0},
		},
		Bitmap{
			0b00000,
			0b01110,
			0b01110,
			0b01110,
			0b00000,
		},
	},
	{
		Point{X: 1, Y: 1},
		Bitmap{
			0b11111,
			0b10001,
			0b10001,
			0b10001,
			0b11111,
		},
		// Single square in the middle with thin walls on all 4 outer sides
		&thinspec{
			N: Bitmap{0, 0, 0, 0b00100, 0},
			S: Bitmap{0, 0b00100, 0, 0, 0},
			E: Bitmap{0, 0, 0b00010, 0, 0},
			W: Bitmap{0, 0, 0b01000, 0, 0},
		},
		Bitmap{
			0b00000,
			0b01110,
			0b01010,
			0b01110,
			0b00000,
		},
	},
}

func TestReachableThin(t *testing.T) {
	var zero Bitmap
	for i, tt := range reachableThinTests {
		mask := tt.mask
		r := reachableThin(tt.pos.X, tt.pos.Y, tt.thin, &mask, &zero)
		if r != tt.reachable {
			t.Errorf("test %d\n%s\n(thin walls omitted)\nwant:\n%s\ngot:\n%s", i, &tt.mask, &tt.reachable, &r)
		}
	}
}
