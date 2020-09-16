package main

type Bitmap [height]uint16

const width = 10
const height = 10

func (b *Bitmap) Set(x, y int8, v bool) {
	if v {
		b[y] |= 1 << uint(x)
	} else {
		b[y] &^= 1 << uint(x)
	}
}

func (b *Bitmap) At(x, y int8) bool {
	return (b[y]>>uint(x))&1 != 0
}

func (b Bitmap) Union(q Bitmap) Bitmap {
	for i := range b {
		b[i] |= q[i]
	}
	return b
}

func (b *Bitmap) String() string {
	var s []byte
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
