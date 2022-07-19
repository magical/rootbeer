// Copyright 2020 Andrew Ekstedt
// This program is licensed under the GNU Affero General
// Public License v3.0. See LICENSE for details.

package main

type BitmapT[Self any] interface {
	Bitmap8 | Bitmap5
	Union(Self) Self
}

type BitmapI[Self BitmapT[Self]] interface {
	*Self
	Set(x, y int8, v bool)
	At(x, y int8) bool
	Slice() []uint16
	String() string
	reachable(x, y int8, mask1, mask2 *Self)
}

type Bitmap8 [8]uint16

const width = 10

//const height = 8

func (b *Bitmap8) Set(x, y int8, v bool) {
	if v {
		b[y] |= 1 << uint(x)
	} else {
		b[y] &^= 1 << uint(x)
	}
}

func (b *Bitmap8) At(x, y int8) bool {
	return (b[y]>>uint(x))&1 != 0
}

func (b *Bitmap8) Slice() []uint16 { return b[:] }

func (b Bitmap8) Union(q Bitmap8) Bitmap8 {
	for i := range b {
		b[i] |= q[i]
	}
	return b
}

func (b *Bitmap8) String() string {
	var s []byte
	const height = len(*b)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if b.At(int8(x), int8(y)) {
				s = append(s, "[]"...)
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

type Bitmap5 [5]uint16

func (b *Bitmap5) Set(x, y int8, v bool) {
	if v {
		b[y] |= 1 << uint(x)
	} else {
		b[y] &^= 1 << uint(x)
	}
}

func (b *Bitmap5) At(x, y int8) bool {
	return (b[y]>>uint(x))&1 != 0
}

func (b *Bitmap5) Slice() []uint16 { return b[:] }

func (b Bitmap5) Union(q Bitmap5) Bitmap5 {
	for i := range b {
		b[i] |= q[i]
	}
	return b
}

func (b *Bitmap5) String() string {
	var s []byte
	const height = len(*b)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if b.At(int8(x), int8(y)) {
				s = append(s, "[]"...)
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
