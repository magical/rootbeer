package main

import (
	"errors"
	"io"
	"log"
	"os"
	"strconv"

	"github.com/magical/littlebyte"
)

type Level struct {
	Title string
	//Password string
	TimeLimit int
	Tiles     [32][32]Tile
}

func SaveLevel(filename string, level *Level) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := saveLevel(f, level); err != nil {
		return err
	}
	return f.Close()
}

const magic = "\xAC\xAA\x02\x00"

func saveLevel(w io.Writer, level *Level) error {
	var b = new(littlebyte.Builder)
	// file header
	b.AddBytes([]byte(magic))
	b.AddUint16(1) // 1 level
	// level header
	// time limit 500, no chips
	b.AddUint16LengthPrefixed(func(b *littlebyte.Builder) {
		b.AddUint16(1)                       // level 1
		b.AddUint16(uint16(level.TimeLimit)) // time limit
		b.AddUint16(0)                       // no chips
		// map start
		b.AddUint16(1)
		// rle encode top layer
		b.AddUint16LengthPrefixed(func(b *littlebyte.Builder) {
			for y := range &level.Tiles {
				for x := 0; x < 32; {
					t, count := countTiles(level.Tiles[y][x:])
					x += count
					switch {
					case count > 3:
						b.AddUint8(0xFF)
						b.AddUint8(uint8(count))
						b.AddUint8(t.encoding())
					default:
						te := t.encoding()
						for i := 0; i < count; i++ {
							b.AddUint8(te)
						}
					}
				}
			}
		})
		// bottom layer (empty)
		b.AddUint16LengthPrefixed(func(b *littlebyte.Builder) {
			b.AddBytes([]byte("\xff\xff\x00\xff\xff\x00\xff\xff\x00\xff\xff\x00\xff\x04\x00"))
		})

		// optional fields
		b.AddUint16LengthPrefixed(func(b *littlebyte.Builder) {
			// level title
			b.AddUint8(3)
			b.AddUint8LengthPrefixed(func(b *littlebyte.Builder) {
				b.AddBytes([]byte(level.Title))
				b.AddUint8(0) // NUL
			})

			// Password
			b.AddUint8(6)
			b.AddUint8LengthPrefixed(func(b *littlebyte.Builder) {
				b.AddUint8('M' ^ 0x99)
				b.AddUint8('A' ^ 0x99)
				b.AddUint8('Z' ^ 0x99)
				b.AddUint8('E' ^ 0x99)
				b.AddUint8(0) // NUL
			})
		})
	})

	bytes, err := b.Bytes()
	if err != nil {
		return err
	}
	_, err = w.Write(bytes)
	return err
}

func countTiles(s []Tile) (Tile, int) {
	// assume s is non-empty
	t := s[0]
	for i := 1; i < len(s); i++ {
		if s[i] != t {
			return t, i
		}
	}
	return t, len(s)
}

type Tile uint8

const (
	Floor = iota
	Wall
	Teleport
	Block
	Exit
	Player
	Fire
)

func (t Tile) encoding() uint8 {
	switch t {
	case Floor:
		return 0x00
	case Wall:
		return 0x01
	case Teleport:
		return 0x29
	case Player:
		return 0x6E
	case Exit:
		return 0x15
	case Block:
		return 0x0A
	case Fire:
		return 0x04
	default:
		panic("invalid tile: " + strconv.Itoa(int(t)))
	}
}

var errEOF = errors.New("level: unexpected end of file")

func DecodeLevel(b []byte) (*Level, error) {
	s := littlebyte.String(b)
	var fileMagic []byte
	if !s.ReadBytes(&fileMagic, 4) {
		return nil, errEOF
	}
	if string(fileMagic) != magic {
		return nil, errors.New("level: invalid magic bytes")
	}
	var nlevels uint16
	if !s.ReadUint16(&nlevels) {
		return nil, errEOF
	}
	if nlevels == 0 {
		return nil, errors.New("level: levelset has no levels")
	}
	if nlevels != 1 {
		return nil, errors.New("level: sets with more than one level are unsupported")
	}
	var levelbytes littlebyte.String
	if !s.ReadUint16LengthPrefixed(&levelbytes) {
		return nil, errEOF
	}

	level, err := readLevel(levelbytes)
	if err != nil {
		return nil, err
	}
	if !s.Empty() {
		return nil, errors.New("level: garbage at end of file")
	}

	return level, nil
}

func readLevel(s littlebyte.String) (*Level, error) {
	// n
	// time limit
	// chips
	// 1
	// top layer
	// bottom layer
	// fields
	var (
		n     uint16
		time  uint16
		chips uint16
		x     uint16
	)
	if !s.ReadUint16(&n) {
		return nil, errEOF
	}
	if n != 1 { // TODO: don't hardcode
		return nil, errors.New("level: expected level 1")
	}
	if !s.ReadUint16(&time) {
		return nil, errEOF
	}
	if !s.ReadUint16(&chips) {
		return nil, errEOF
	}
	if !s.ReadUint16(&x) {
		return nil, errEOF
	}
	if x != 1 {
		return nil, errors.New("level: malformed level")
	}

	var level Level
	level.TimeLimit = int(time)

	//
	var (
		topdata    littlebyte.String
		bottomdata littlebyte.String
		fields     littlebyte.String
	)
	var printedWarning = false
	if !s.ReadUint16LengthPrefixed(&topdata) {
		return nil, errEOF
	}
	// decode top layer RLE data
	for i := 0; i < 32*32; {
		var t uint8
		if !topdata.ReadUint8(&t) {
			return nil, errors.New("level: unexpected end of layer data")
		}
		var count uint8 = 1
		if t == 0xFF {
			if !topdata.ReadUint8(&count) {
				return nil, errors.New("level: unexpected end of layer data")
			}
			if !topdata.ReadUint8(&t) {
				return nil, errors.New("level: unexpected end of layer data")
			}
		} else {
			count = 1
		}
		var tile Tile = Floor
		switch t {
		case 0x00:
			tile = Floor
		case 0x01:
			tile = Wall
		case 0x0A:
			tile = Block
		case 0x29:
			tile = Teleport
		case 0x6C, 0x6D, 0x6E, 0x6F:
			tile = Player
		case 0x15:
			tile = Exit
		case 0x04:
			tile = Fire
		default:
			// unknown tile
			if !printedWarning {
				log.Printf("warning: unknown tile %#x at %d,%d (only one warning will be shown per level)", t, i%32, i/32)
				printedWarning = true
			}
		}
		for j := 0; j < int(count); j++ {
			x, y := i%32, i/32
			level.Tiles[y][x] = tile
			i++
		}
	}
	if !topdata.Empty() {
		return nil, errors.New("level: garbage at end of top layer data")
	}

	// ignore bottom layer
	if !s.ReadUint16LengthPrefixed(&bottomdata) {
		return nil, errEOF
	}

	if !s.ReadUint16LengthPrefixed(&fields) {
		return nil, errEOF
	}

	if !s.Empty() {
		return nil, errors.New("level: garbage at end of level")
	}
	return &level, nil
}
