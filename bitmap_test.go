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
