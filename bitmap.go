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

// Reports whether every point not in m is reachable from a
func reachable(m *Bitmap, a Point) bool {
	panic("unused")
	b := m.Floodfill(a.X, a.Y)
	for y := range b {
		if b[y]|m[y] != ^border[y] {
			return false
		}
	}
	return true
}

func (b Bitmap) Union(q Bitmap) Bitmap {
	for i := range b {
		b[i] |= q[i]
	}
	return b
}

func (mask *Bitmap) Floodfill(x, y int8) Bitmap {
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
			tmp2 &^= mask[i]
			tmp2 &^= border[i]
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
