package main

type Bitmap [16]uint16

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
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if b.At(int8(x), int8(y)) {
				if (x+y)%2 == 0 {
					s = append(s, "##"...)
				} else {
					s = append(s, "&&"...)
				}
			} else {
				if (x+y)%2 == 0 {
					s = append(s, ".."...)
				} else {
					s = append(s, ",,"...)
				}
			}
		}
		s = append(s, '\n')
	}
	return string(s)
}
